package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/client/connection"
	"github.com/QingYu-Su/Yui/internal/client/handlers/subsystems"
	"github.com/QingYu-Su/Yui/internal/terminal"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"github.com/QingYu-Su/Yui/pkg/storage"

	"golang.org/x/crypto/ssh"
)

// exit 发送SSH会话退出状态码
// 参数:
//
//	session - SSH通道对象，用于发送退出状态
//	code - 退出状态码(通常0表示成功，非0表示错误)
func exit(session ssh.Channel, code int) {
	// 构造退出状态数据结构(RFC 4254 6.10)
	status := struct{ Status uint32 }{uint32(code)}

	// 发送SSH退出状态请求
	// 参数说明:
	//   "exit-status" - 请求类型
	//   false - 表示不需要回复
	//   ssh.Marshal(&status) - 序列化的状态数据
	session.SendRequest("exit-status", false, ssh.Marshal(&status))
}

// Session 处理SSH会话通道的各种请求类型
// 参数:
//
//	session - 包含会话状态和连接信息的结构体
//
// 返回值:
//
//	返回一个函数，用于处理新通道上的SSH请求
func Session(session *connection.Session) func(newChannel ssh.NewChannel, log logger.Logger) {

	return func(newChannel ssh.NewChannel, log logger.Logger) {
		defer log.Info("Session disconnected") // 会话结束时记录日志

		// 接受新通道并获取请求通道
		connection, requests, err := newChannel.Accept()
		if err != nil {
			log.Warning("无法接受通道 (%s)", err)
			return
		}
		defer func() {
			exit(connection, 0) // 发送退出状态码
			connection.Close()  // 确保通道关闭
		}()

		// 处理通道上的所有请求
		for req := range requests {
			log.Info("会话收到请求: %q", req.Type)

			switch req.Type {
			case "subsystem":
				// 处理SSH子系统请求(sftp等)
				err := subsystems.RunSubsystems(connection, req)
				if err != nil {
					log.Error("子系统执行错误: %s", err.Error())
					fmt.Fprintf(connection, "子系统错误: '%s'", err.Error())
				}
				return

			case "exec":
				// 处理远程命令执行请求
				var cmd internal.ShellStruct
				err = ssh.Unmarshal(req.Payload, &cmd)
				if err != nil {
					log.Warning("客户端发送了无法解析的exec载荷: %s\n", err)
					req.Reply(false, nil)
					return
				}

				req.Reply(true, nil) // 确认请求

				// 解析命令行
				line := terminal.ParseLine(cmd.Cmd, 0)
				if line.Empty() {
					log.Warning("客户端发送了空命令: %s\n", err)
					return
				}

				command := line.Command.Value()

				// 特殊处理scp命令
				if command == "scp" {
					scp(line.Chunks[1:], connection, log)
					return
				}

				// 检查是否是URL格式命令(支持远程下载执行)
				u, ok := isUrl(command)
				if ok {
					command, err = download(session.ServerConnection, u)
					if err != nil {
						fmt.Fprintf(connection, "%s", err.Error())
						return
					}
				}

				// 根据是否分配了PTY选择执行方式
				if session.Pty != nil {
					runCommandWithPty(u.Query().Get("argv"), command, line.Chunks[1:], session.Pty, requests, log, connection)
					return
				}
				runCommand(u.Query().Get("argv"), command, line.Chunks[1:], connection)
				return

			case "shell":
				// 处理交互式shell请求
				req.Reply(true, nil) // 确认请求

				var shellPath internal.ShellStruct
				err := ssh.Unmarshal(req.Payload, &shellPath)
				// 如果命令为空或解析失败，启动交互式shell
				if err != nil || shellPath.Cmd == "" {
					shell(session.Pty, connection, requests, log)
					return
				}

				// 处理带命令的shell请求
				parts := strings.Split(shellPath.Cmd, " ")
				if len(parts) > 0 {
					command := parts[0]
					u, ok := isUrl(parts[0])
					if ok {
						command, err = download(session.ServerConnection, u)
						if err != nil {
							fmt.Fprintf(connection, "%s", err.Error())
							return
						}
					}
					runCommandWithPty(u.Query().Get("argv"), command, parts[1:], session.Pty, requests, log, connection)
				}
				return

			case "pty-req":
				// 处理终端(PTY)分配请求
				pty, err := internal.ParsePtyReq(req.Payload)
				if err != nil {
					log.Warning("收到无法解析的pty请求: %s", err)
					req.Reply(false, nil)
					return
				}
				session.Pty = &pty   // 保存PTY配置到会话
				req.Reply(true, nil) // 确认请求

			default:
				// 处理未知请求类型
				log.Warning("收到未知请求 %s", req.Type)
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}
	}
}

// runCommand 执行指定命令并将输入/输出重定向到SSH通道
// 参数:
//
//	argv - 可选的命令参数覆盖
//	command - 要执行的命令路径或名称
//	args - 命令参数列表
//	connection - SSH通道，用于I/O重定向
func runCommand(argv string, command string, args []string, connection ssh.Channel) {
	// 1. 确保PATH环境变量已设置
	if len(os.Getenv("PATH")) == 0 {
		if runtime.GOOS != "windows" {
			// Linux默认PATH
			os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/bin:/bin:/sbin")
		} else {
			// Windows默认PATH
			os.Setenv("PATH", "C:\\Windows\\system32;C:\\Windows;C:\\Windows\\System32\\Wbem;C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\")
		}
	}

	// 2. 创建命令对象
	cmd := exec.Command(command, args...)
	if len(argv) != 0 {
		cmd.Args[0] = argv // 覆盖第一个参数（如果有指定）
	}

	// 3. 设置标准输出管道
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(connection, "标准输出管道错误: %s", err.Error())
		return
	}
	defer stdout.Close()

	// 4. 将标准错误重定向到标准输出
	cmd.Stderr = cmd.Stdout

	// 5. 设置标准输入管道
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(connection, "标准输入管道错误: %s", err.Error())
		return
	}
	defer stdin.Close()

	// 6. 启动goroutine处理I/O重定向
	go io.Copy(stdin, connection)  // SSH输入 → 命令输入
	go io.Copy(connection, stdout) // 命令输出 → SSH输出

	// 7. 执行命令并等待完成
	err = cmd.Run()
	if err != nil {
		fmt.Fprintf(connection, "命令执行错误: %s", err.Error())
		return
	}
}

// isUrl 检查字符串是否为合法URL
// 参数:
//
//	data - 待检查的字符串
//
// 返回值:
//
//	*url.URL - 解析后的URL对象
//	bool - 是否为支持的URL类型
func isUrl(data string) (*url.URL, bool) {
	u, err := url.Parse(data)
	if err != nil {
		return u, false
	}

	// 只支持http/https/rssh协议
	switch u.Scheme {
	case "http", "https", "rssh":
		return u, true
	}
	return u, false
}

// download 从指定URL下载文件
// 参数:
//
//	serverConnection - SSH连接对象(用于rssh协议)
//	fromUrl - 要下载的URL
//
// 返回值:
//
//	string - 下载文件的本地路径
//	error - 下载过程中的错误
func download(serverConnection ssh.Conn, fromUrl *url.URL) (result string, err error) {
	if fromUrl == nil {
		return "", errors.New("URL不能为空")
	}

	var (
		reader   io.ReadCloser // 下载内容读取器
		filename string        // 本地保存文件名
	)

	// 1. 复制URL对象避免修改原始参数
	urlCopy := *fromUrl

	// 2. 处理查询参数
	query := urlCopy.Query()
	query.Del("argv") // 移除特殊参数
	urlCopy.RawQuery = query.Encode()

	// 3. 根据协议类型处理下载
	switch urlCopy.Scheme {
	case "http", "https":
		// HTTP/HTTPS下载处理
		resp, err := http.Get(urlCopy.String())
		if err != nil {
			return "", fmt.Errorf("HTTP请求失败: %w", err)
		}
		defer resp.Body.Close()

		reader = resp.Body
		filename = path.Base(urlCopy.Path)
		if filename == "." {
			// 如果URL没有明确文件名，生成随机文件名
			filename, err = internal.RandomString(16)
			if err != nil {
				return "", fmt.Errorf("生成随机文件名失败: %w", err)
			}
		}

	case "rssh":
		// RSSH协议处理(通过SSH通道下载)
		filename = path.Base(strings.TrimSuffix(urlCopy.String(), "rssh://"))

		// 打开专用SSH通道进行文件传输
		ch, reqs, err := serverConnection.OpenChannel("rssh-download", []byte(filename))
		if err != nil {
			return "", fmt.Errorf("打开SSH传输通道失败: %w", err)
		}
		go ssh.DiscardRequests(reqs) // 丢弃不需要的通道请求

		reader = ch

	default:
		return "", fmt.Errorf("不支持的协议类型: %s", fromUrl.Scheme)
	}

	// 4. 存储下载内容到本地文件
	return storage.Store(filename, reader)
}
