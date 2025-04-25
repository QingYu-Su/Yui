package client

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/user"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/client/connection"
	"github.com/QingYu-Su/Yui/internal/client/handlers"
	"github.com/QingYu-Su/Yui/internal/client/keys"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
	socks "golang.org/x/net/proxy"
	"golang.org/x/net/websocket"
)

// WriteHTTPReq 向连接写入HTTP请求
// 参数:
//
//	lines - HTTP请求行(包括请求头)
//	conn - 网络连接对象
//
// 返回值:
//
//	error - 如果写入失败则返回错误
func WriteHTTPReq(lines []string, conn net.Conn) error {
	lines = append(lines, "") // 添加空行表示HTTP请求结束
	for _, line := range lines {
		// 写入每一行数据并添加CRLF换行符
		n, err := conn.Write([]byte(line + "\r\n"))
		if err != nil {
			return err
		}

		// 检查是否完整写入
		if len(line+"\r\n") < n {
			return io.ErrShortWrite
		}
	}
	return nil
}

// GetProxyDetails 解析并规范化代理地址
// 参数:
//
//	proxy - 原始代理地址字符串
//
// 返回值:
//
//	string - 规范化后的代理URL
//	error - 如果解析失败则返回错误
//
// 注: 此函数复制自golang.org/x/net/httpproxy，因为原代码不保证向后兼容性
func GetProxyDetails(proxy string) (string, error) {
	if proxy == "" {
		return "", nil
	}

	// 尝试直接解析代理地址
	proxyURL, err := url.Parse(proxy)
	if err != nil ||
		(proxyURL.Scheme != "http" &&
			proxyURL.Scheme != "https" &&
			proxyURL.Scheme != "socks" &&
			proxyURL.Scheme != "socks5" &&
			proxyURL.Scheme != "socks4") {
		// 如果解析失败，尝试添加http://前缀再次解析
		proxyURL, err = url.Parse("http://" + proxy)
	}

	if err != nil {
		return "", fmt.Errorf("无效的代理地址 %q: %v", proxy, err)
	}

	// 如果代理URL没有指定端口，根据协议添加默认端口
	port := proxyURL.Port()
	if port == "" {
		switch proxyURL.Scheme {
		case "socks5", "socks", "socks4":
			proxyURL.Host += ":1080" // SOCKS代理默认端口
		case "https":
			proxyURL.Host += ":443" // HTTPS默认端口
		case "http":
			proxyURL.Host += ":80" // HTTP默认端口
		}
	}

	// 返回规范化后的代理URL(协议://主机:端口)
	return proxyURL.Scheme + "://" + proxyURL.Host, nil
}

// Connect 建立到目标地址的网络连接，支持通过代理连接
// 参数:
//
//	addr - 目标服务器地址(格式: host:port)
//	proxy - 代理服务器地址(格式: scheme://host:port)
//	timeout - 连接超时时间
//	winauth - 是否使用Windows身份验证
//
// 返回值:
//
//	net.Conn - 建立的网络连接
//	error - 如果连接失败则返回错误
func Connect(addr, proxy string, timeout time.Duration, winauth bool) (conn net.Conn, err error) {
	// 如果指定了代理服务器
	if len(proxy) != 0 {
		log.Println("设置HTTP代理地址为: ", proxy)
		proxyURL, _ := url.Parse(proxy) // 代理地址已经预先解析过

		// HTTP/HTTPS代理处理
		if proxyURL.Scheme == "http" || proxyURL.Scheme == "https" {
			var (
				proxyCon net.Conn
				err      error
			)
			// 根据代理协议类型建立连接
			switch proxyURL.Scheme {
			case "http":
				// 普通HTTP代理连接
				proxyCon, err = net.DialTimeout("tcp", proxyURL.Host, timeout)
			case "https":
				// HTTPS代理连接，跳过证书验证
				proxyCon, err = tls.DialWithDialer(&net.Dialer{
					Timeout: timeout,
				}, "tcp", proxyURL.Host, &tls.Config{
					InsecureSkipVerify: true,
				})
			}
			if err != nil {
				return nil, err
			}

			// 设置TCP保持连接
			if tcpC, ok := proxyCon.(*net.TCPConn); ok {
				tcpC.SetKeepAlivePeriod(2 * time.Hour)
			}

			// 第一次尝试无认证的CONNECT请求
			req := []string{
				fmt.Sprintf("CONNECT %s HTTP/1.1", addr),
				fmt.Sprintf("Host: %s", addr),
			}

			// 发送HTTP请求
			err = WriteHTTPReq(req, proxyCon)
			if err != nil {
				return nil, fmt.Errorf("无法连接到代理 %s", proxy)
			}

			// 读取代理服务器响应
			var responseStatus []byte
			for {
				b := make([]byte, 1)
				_, err := proxyCon.Read(b)
				if err != nil {
					return conn, fmt.Errorf("从代理读取失败")
				}
				responseStatus = append(responseStatus, b...)

				// 检测HTTP响应结束(\r\n\r\n)
				if len(responseStatus) > 4 && bytes.Equal(responseStatus[len(responseStatus)-4:], []byte("\r\n\r\n")) {
					break
				}
			}

			// 处理407代理认证要求
			if bytes.Contains(bytes.ToLower(responseStatus), []byte("407")) {
				// 检查是否支持NTLM认证
				if bytes.Contains(bytes.ToLower(responseStatus), []byte("proxy-authenticate: ntlm")) {
					if ntlmProxyCreds != "" {
						// NTLM认证流程开始

						// 1. 发送NTLM协商消息(Type 1)
						ntlmHeader, err := getNTLMAuthHeader(nil)
						if err != nil {
							return nil, fmt.Errorf("NTLM协商失败: %v", err)
						}

						req = []string{
							fmt.Sprintf("CONNECT %s HTTP/1.1", addr),
							fmt.Sprintf("Host: %s", addr),
							fmt.Sprintf("Proxy-Authorization: %s", ntlmHeader),
						}

						err = WriteHTTPReq(req, proxyCon)
						if err != nil {
							return nil, fmt.Errorf("发送NTLM协商消息失败: %s", err)
						}

						// 2. 读取NTLM挑战响应(Type 2)
						responseStatus = []byte{}
						for {
							b := make([]byte, 1)
							_, err := proxyCon.Read(b)
							if err != nil {
								return conn, fmt.Errorf("读取NTLM挑战失败")
							}
							responseStatus = append(responseStatus, b...)

							if len(responseStatus) > 4 && bytes.Equal(responseStatus[len(responseStatus)-4:], []byte("\r\n\r\n")) {
								break
							}
						}

						// 解析挑战消息
						ntlmParts := strings.SplitN(string(responseStatus), NTLM, 2)
						if len(ntlmParts) != 2 {
							return nil, fmt.Errorf("未收到NTLM挑战")
						}

						challengeStr := strings.SplitN(ntlmParts[1], "\r\n", 2)[0]
						challenge, err := base64.StdEncoding.DecodeString(challengeStr)
						if err != nil {
							return nil, fmt.Errorf("无效的NTLM挑战: %v", err)
						}

						// 3. 生成并发送NTLM认证消息(Type 3)
						ntlmHeader, err = getNTLMAuthHeader(challenge)
						if err != nil {
							return nil, fmt.Errorf("NTLM认证失败: %v", err)
						}

						req = []string{
							fmt.Sprintf("CONNECT %s HTTP/1.1", addr),
							fmt.Sprintf("Host: %s", addr),
							fmt.Sprintf("Proxy-Authorization: %s", ntlmHeader),
						}

						err = WriteHTTPReq(req, proxyCon)
						if err != nil {
							return nil, fmt.Errorf("发送NTLM认证消息失败: %v", err)
						}

						// 4. 读取最终响应
						responseStatus = []byte{}
						for {
							b := make([]byte, 1)
							_, err := proxyCon.Read(b)
							if err != nil {
								return conn, fmt.Errorf("读取最终响应失败")
							}
							responseStatus = append(responseStatus, b...)

							if len(responseStatus) > 4 && bytes.Equal(responseStatus[len(responseStatus)-4:], []byte("\r\n\r\n")) {
								break
							}
						}
					} else if winauth {
						// Windows身份验证流程
						req = additionalHeaders(proxy, req)
						err = WriteHTTPReq(req, proxyCon)
						if err != nil {
							return nil, fmt.Errorf("无法连接到代理 %s", proxy)
						}

						responseStatus = []byte{}
						for {
							b := make([]byte, 1)
							_, err := proxyCon.Read(b)
							if err != nil {
								return conn, fmt.Errorf("从代理读取失败")
							}
							responseStatus = append(responseStatus, b...)

							if len(responseStatus) > 4 && bytes.Equal(responseStatus[len(responseStatus)-4:], []byte("\r\n\r\n")) {
								break
							}
						}
					}
				}
			}

			// 检查最终响应状态码是否为200
			if !(bytes.Contains(bytes.ToLower(responseStatus), []byte("200"))) {
				parts := bytes.Split(responseStatus, []byte("\r\n"))
				if len(parts) > 1 {
					return nil, fmt.Errorf("代理连接失败: %q", parts[0])
				}
			}

			log.Println("代理接受CONNECT请求，连接建立成功!")

			return proxyCon, nil
		}

		// SOCKS代理处理
		if proxyURL.Scheme == "socks" || proxyURL.Scheme == "socks5" {
			// 创建SOCKS5拨号器
			dial, err := socks.SOCKS5("tcp", proxyURL.Host, nil, nil)
			if err != nil {
				return nil, fmt.Errorf("SOCKS连接失败: %s", err)
			}
			// 通过SOCKS代理建立连接
			proxyCon, err := dial.Dial("tcp", addr)
			if err != nil {
				return nil, fmt.Errorf("SOCKS拨号失败: %s", err)
			}

			log.Println("SOCKS代理连接建立成功!")

			return proxyCon, nil
		}
	}

	// 无代理直接连接
	conn, err = net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %s", err)
	}

	// 设置TCP保持连接
	if tcpC, ok := conn.(*net.TCPConn); ok {
		tcpC.SetKeepAlivePeriod(2 * time.Hour)
	}

	return conn, nil
}

// getCaseInsensitiveEnv 获取环境变量值（不区分大小写）
// 参数:
//
//	envs - 需要查找的环境变量名列表（支持多个）
//
// 返回值:
//
//	[]string - 匹配的环境变量值列表（按输入顺序返回）
//
// 示例:
//
//	// 假设环境变量中有 Path=/bin 和 PATH=/usr/bin
//	values := getCaseInsensitiveEnv("path", "PATH")
//	// 可能返回 ["/bin", "/usr/bin"] 或 ["/usr/bin", "/bin"] 取决于环境变量的顺序
func getCaseInsensitiveEnv(envs ...string) (ret []string) {
	// 创建小写环境变量名的查找表
	lower := map[string]bool{}
	for _, env := range envs {
		lower[strings.ToLower(env)] = true
	}

	// 遍历所有环境变量
	for _, e := range os.Environ() {
		// 分割环境变量名和值
		part := strings.SplitN(e, "=", 2)
		// 检查环境变量名是否在查找表中（不区分大小写）
		if len(part) > 1 && lower[strings.ToLower(part[0])] {
			ret = append(ret, part[1]) // 添加匹配的环境变量值
		}
	}
	return ret
}

// Run 是客户端主运行函数，负责建立和维护与服务器的连接
// 参数:
//
//	addr - 服务器地址
//	fingerprint - 服务器公钥指纹
//	proxyAddr - 代理服务器地址
//	sni - TLS SNI(服务器名称指示)
//	winauth - 是否使用Windows身份验证
func Run(addr, fingerprint, proxyAddr, sni string, winauth bool) {
	// 1. 获取SSH私钥
	sshPriv, sysinfoError := keys.GetPrivateKey()
	if sysinfoError != nil {
		log.Fatal("获取私钥失败: ", sysinfoError)
	}

	// 初始化日志记录器
	l := logger.NewLog("client")

	// 2. 解析代理地址
	var err error
	proxyAddr, err = GetProxyDetails(proxyAddr)
	if err != nil {
		log.Fatal("无效的代理地址", proxyAddr, ":", err)
	}

	// 3. 获取当前用户信息
	var username string
	userInfo, sysinfoError := user.Current()
	if sysinfoError != nil {
		l.Warning("无法获取用户名: %s", sysinfoError.Error())
		username = "Unknown"
	} else {
		username = userInfo.Username
	}

	// 4. 获取主机名
	hostname, sysinfoError := os.Hostname()
	if sysinfoError != nil {
		hostname = "Unknown Hostname"
		l.Warning("无法获取主机名: %s", sysinfoError)
	}

	// 5. 配置SSH客户端
	config := &ssh.ClientConfig{
		User: fmt.Sprintf("%s.%s", username, hostname), // 使用"用户名.主机名"格式
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(sshPriv), // 使用公钥认证
		},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			// 服务器公钥验证逻辑
			if fingerprint == "" {
				l.Warning("未指定服务器密钥，允许连接到 %s", addr)
				return nil
			}

			if internal.FingerprintSHA256Hex(key) != fingerprint {
				return fmt.Errorf("服务器公钥无效，期望: %s，实际: %s", fingerprint, internal.FingerprintSHA256Hex(key))
			}

			return nil
		},
		ClientVersion: "SSH-" + internal.Version + "-" + runtime.GOOS + "_" + runtime.GOARCH,
	}

	// 6. 确定连接类型(stdio/tls/ws等)
	realAddr, scheme := determineConnectionType(addr)

	// 7. 从环境变量获取备用代理列表
	potentialProxies := getCaseInsensitiveEnv("http_proxy", "https_proxy")
	triedProxyIndex := 0
	initialProxyAddr := proxyAddr

	// 8. 主连接循环
	for {
		var conn net.Conn
		if scheme != "stdio" {
			log.Println("正在连接到", addr)

			// 8.1 建立原始TCP连接
			conn, err = Connect(realAddr, proxyAddr, config.Timeout, winauth)
			if err != nil {
				// 处理连接错误
				if errMsg := err.Error(); strings.Contains(errMsg, "missing port in address") {
					log.Fatalf("无法连接到TCP，无效的地址: '%s', %s", addr, errMsg)
				}

				log.Printf("无法直接连接TCP: %s\n", err)

				// 尝试使用环境变量中的代理
				if len(potentialProxies) > 0 {
					if len(potentialProxies) <= triedProxyIndex {
						log.Printf("无法通过代理连接(来自环境变量)，正在重试代理 %q", initialProxyAddr)
						triedProxyIndex = 0
						proxyAddr = initialProxyAddr
						continue
					}
					proxy := potentialProxies[triedProxyIndex]
					triedProxyIndex++

					log.Println("正在尝试通过环境变量中的代理连接(", proxy, ")")

					proxyAddr, err = GetProxyDetails(proxy)
					if err != nil {
						log.Println("无法解析环境变量中的代理值: ", proxy)
					}
					continue
				}

				<-time.After(10 * time.Second)
				continue
			}

			// 8.2 根据协议类型添加传输层
			if scheme == "tls" || scheme == "wss" || scheme == "https" {
				// TLS连接处理
				sniServerName := sni
				if len(sni) == 0 {
					sniServerName = realAddr
					parts := strings.Split(realAddr, ":")
					if len(parts) == 2 {
						sniServerName = parts[0]
					}
				}

				clientTlsConn := tls.Client(conn, &tls.Config{
					InsecureSkipVerify: true,
					ServerName:         sniServerName,
				})
				err = clientTlsConn.Handshake()
				if err != nil {
					log.Printf("无法连接TLS: %s\n", err)
					<-time.After(10 * time.Second)
					continue
				}

				conn = clientTlsConn
			}

			// 8.3 处理WebSocket连接
			switch scheme {
			case "wss", "ws":
				c, err := websocket.NewConfig("ws://"+realAddr+"/ws", "ws://"+realAddr)
				if err != nil {
					log.Println("无法创建WebSocket配置: ", err)
					<-time.After(10 * time.Second)
					continue
				}

				wsConn, err := websocket.NewClient(c, conn)
				if err != nil {
					log.Printf("无法连接WebSocket: %s\n", err)
					<-time.After(10 * time.Second)
					continue
				}
				wsConn.PayloadType = websocket.BinaryFrame
				conn = wsConn

			case "http", "https":
				// HTTP连接处理
				conn, err = NewHTTPConn(scheme+"://"+realAddr, func() (net.Conn, error) {
					return Connect(realAddr, proxyAddr, config.Timeout, winauth)
				})

				if err != nil {
					log.Printf("无法连接HTTP: %s\n", err)
					<-time.After(10 * time.Second)
					continue
				}
			}
		} else {
			// 标准输入输出模式
			conn = &InetdConn{}
		}

		// 9. 设置连接超时(初始较长以便用户输入SSH公钥)
		realConn := &internal.TimeoutConn{Conn: conn, Timeout: 4 * time.Minute}

		// 10. 建立SSH客户端连接
		sshConn, chans, reqs, err := ssh.NewClientConn(realConn, addr, config)
		if err != nil {
			realConn.Close()
			log.Printf("无法启动新的客户端连接: %s\n", err)

			if scheme == "stdio" {
				// 如果是标准输入输出模式，连接失败直接退出
				return
			}

			<-time.After(10 * time.Second)
			continue
		}

		// 11. 连接成功后重置代理计数器
		if len(potentialProxies) > 0 {
			triedProxyIndex = 0
		}

		log.Println("成功连接到", addr)

		// 12. 处理SSH全局请求
		go func() {
			for req := range reqs {
				switch req.Type {
				case "kill":
					// 处理kill命令
					log.Println("收到kill命令，即将退出")
					<-time.After(5 * time.Second)
					os.Exit(0)

				case "keepalive-rssh@golang.org":
					// 处理心跳包
					req.Reply(false, nil)
					timeout, err := strconv.Atoi(string(req.Payload))
					if err != nil {
						continue
					}
					realConn.Timeout = time.Duration(timeout*2) * time.Second

				case "log-level":
					// 处理日志级别设置
					u, err := logger.StrToUrgency(string(req.Payload))
					if err != nil {
						log.Printf("服务器发送了无效的日志级别: %q", string(req.Payload))
						req.Reply(false, nil)
						continue
					}
					logger.SetLogLevel(u)
					req.Reply(true, nil)

				case "log-to-file":
					// 处理日志文件输出
					req.Reply(true, nil)
					if err := handlers.Console.ToFile(string(req.Payload)); err != nil {
						log.Println("无法将日志输出到文件 ", string(req.Payload), err)
					}

				case "tcpip-forward":
					// 处理远程端口转发
					go handlers.StartRemoteForward(nil, req, sshConn)

				case "query-tcpip-forwards":
					// 查询现有的远程端口转发
					f := struct {
						RemoteForwards []string
					}{
						RemoteForwards: handlers.GetServerRemoteForwards(),
					}
					req.Reply(true, ssh.Marshal(f))

				case "cancel-tcpip-forward":
					// 取消远程端口转发
					var rf internal.RemoteForwardRequest
					err := ssh.Unmarshal(req.Payload, &rf)
					if err != nil {
						req.Reply(false, []byte(fmt.Sprintf("无法解析远程转发请求: %s", err.Error())))
						return
					}

					go func(r *ssh.Request) {
						err := handlers.StopRemoteForward(rf)
						if err != nil {
							r.Reply(false, []byte(err.Error()))
							return
						}
						r.Reply(true, nil)
					}(req)

				default:
					// 处理其他未知请求
					if req.WantReply {
						req.Reply(false, nil)
					}
				}
			}
		}()

		// 13. 注册通道回调处理
		clientLog := logger.NewLog("client")
		err = connection.RegisterChannelCallbacks(chans, clientLog, map[string]func(newChannel ssh.NewChannel, log logger.Logger){
			"session":        handlers.Session(connection.NewSession(sshConn)), // 会话处理
			"jump":           handlers.JumpHandler(sshPriv, sshConn),           // 跳板机处理
			"log-to-console": handlers.LogToConsole,                            // 控制台日志
		})

		// 14. 清理资源
		sshConn.Close()
		handlers.StopAllRemoteForwards()

		if err != nil {
			log.Printf("服务器意外断开: %s\n", err)

			if scheme == "stdio" {
				return
			}

			<-time.After(10 * time.Second)
			continue
		}
	}
}

// matchSchemeDefinition 用于匹配URL中的协议部分(如 http://)
var matchSchemeDefinition = regexp.MustCompile(`.*\:\/\/`)

// determineConnectionType 解析连接地址并确定连接类型和实际地址
// 参数:
//
//	addr - 原始连接地址(可能包含协议前缀)
//
// 返回值:
//
//	resultingAddr - 处理后的实际连接地址(包含端口)
//	transport - 连接类型/协议(ssh/tls/ws等)
func determineConnectionType(addr string) (resultingAddr, transport string) {
	// 1. 检查地址是否包含协议定义
	if !matchSchemeDefinition.MatchString(addr) {
		// 如果不包含协议前缀，默认使用SSH协议
		return addr, "ssh"
	}

	// 2. 尝试解析为URL
	u, err := url.ParseRequestURI(addr)
	if err != nil {
		// 如果解析失败(如格式为1.1.1.1:4343)，默认使用SSH协议
		return addr, "ssh"
	}

	// 3. 处理无协议的情况
	if u.Scheme == "" {
		// 如果只有IP地址没有端口，添加默认SSH端口22
		log.Println("未指定端口: ", u.Path, "使用默认端口22")
		return u.Path + ":22", "ssh"
	}

	// 4. 处理无端口的情况
	if u.Port() == "" {
		// 根据协议类型设置默认端口
		switch u.Scheme {
		case "tls", "wss":
			// TLS/WebSocket Secure 默认使用443端口
			return u.Host + ":443", u.Scheme
		case "ws":
			// WebSocket 默认使用80端口
			return u.Host + ":80", u.Scheme
		case "stdio":
			// 标准输入输出模式，地址不重要
			return "stdio://nothing", u.Scheme
		}

		// 未知协议类型，回退到SSH默认端口22
		log.Println("URL协议 ", u.Scheme, " 无法识别，回退到SSH协议: ", u.Host+":22", " (未指定端口)")
		return u.Host + ":22", "ssh"
	}

	// 5. 正常情况(包含协议和端口)
	return u.Host, u.Scheme
}
