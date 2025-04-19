package handlers

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server/commands"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/internal/server/webserver"
	"github.com/QingYu-Su/Yui/internal/terminal"
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// sendExitCode 向SSH通道发送退出状态码
// 参数:
//   - code: 要发送的退出状态码(32位无符号整数)
//   - channel: 目标SSH通道
//
// 功能:
//
//	按照SSH协议格式发送"exit-status"请求，包含4字节大端序的状态码
func sendExitCode(code uint32, channel ssh.Channel) {
	// 创建4字节缓冲区
	b := make([]byte, 4)
	// 将状态码转为大端序字节序列
	binary.BigEndian.PutUint32(b, code)
	// 发送exit-status请求
	channel.SendRequest("exit-status", false, b)
}

// Session 函数创建并返回一个ChannelHandler，用于处理SSH会话通道
func Session(datadir string) ChannelHandler {
	// 返回实际的通道处理函数
	return func(connectionDetails string, user *users.User, newChannel ssh.NewChannel, log logger.Logger) {
		// 1. 初始化用户会话
		sess, err := user.Session(connectionDetails)
		if err != nil {
			log.Warning("Could not get user session for %s: err: %s", connectionDetails, err)
			return
		}
		defer log.Info("Session disconnected: %s", sess.ConnectionDetails)

		// 2. 接受通道连接
		connection, requests, err := newChannel.Accept()
		if err != nil {
			log.Warning("Could not accept channel (%s)", err)
			return
		}
		defer connection.Close()

		// 3. 设置会话的请求处理通道
		sess.ShellRequests = requests

		// 4. 处理客户端请求
		for req := range requests {
			log.Info("Session got request: %q", req.Type)

			switch req.Type {
			// 处理"exec"请求 - 执行单条命令
			case "exec":
				var command struct {
					Cmd string
				}
				// 解析命令负载
				err = ssh.Unmarshal(req.Payload, &command)
				if err != nil {
					log.Warning("Human client sent an undecodable exec payload: %s\n", err)
					req.Reply(false, nil)
					return
				}

				// 解析命令行
				line := terminal.ParseLine(command.Cmd, 0)
				if line.Command != nil {
					// 创建命令处理器
					c := commands.CreateCommands(sess.ConnectionDetails, user, log, datadir)

					// 查找并执行对应命令
					if m, ok := c[line.Command.Value()]; ok {
						req.Reply(true, nil)
						err := m.Run(user, connection, line)
						if err != nil {
							sendExitCode(1, connection)
							fmt.Fprintf(connection, "%s", err.Error())
							return
						}
						sendExitCode(0, connection)
						return
					}
				}
				// 命令未找到
				req.Reply(false, []byte("Unknown RSSH command"))
				sendExitCode(1, connection)
				return

			// 处理"shell"请求 - 启动交互式shell
			case "shell":
				// 验证shell请求是否有效
				req.Reply(len(req.Payload) == 0, nil)

				// 创建高级终端实例
				term := terminal.NewAdvancedTerminal(connection, user, sess, internal.ConsoleLabel+"$ ")

				// 设置终端尺寸
				if sess.Pty != nil {
					term.SetSize(int(sess.Pty.Columns), int(sess.Pty.Rows))
				}

				// 配置自动补全
				term.AddValueAutoComplete(autocomplete.RemoteId, user.Autocomplete(), users.PublicClientsAutoComplete)
				term.AddValueAutoComplete(autocomplete.WebServerFileIds, webserver.Autocomplete)

				// 添加可用命令
				term.AddCommands(commands.CreateCommands(sess.ConnectionDetails, user, log, datadir))

				// 运行终端
				err := term.Run()
				if err != nil && err != io.EOF {
					sendExitCode(1, connection)
					log.Error("Error: %s", err)
				}
				sendExitCode(0, connection)
				return

			// 处理"pty-req"请求 - 伪终端请求
			case "pty-req":
				// 解析PTY请求
				pty, err := internal.ParsePtyReq(req.Payload)
				if err != nil {
					log.Warning("Got undecodable pty request: %s", err)
					req.Reply(false, nil)
					return
				}
				// 保存PTY配置到会话
				sess.Pty = &pty
				req.Reply(true, nil)

			// 处理其他不支持请求类型
			default:
				log.Warning("Unsupported request %s", req.Type)
				if req.WantReply {
					req.Reply(false, []byte("Unsupported request"))
				}
			}
		}
	}
}
