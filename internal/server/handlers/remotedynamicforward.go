package handlers

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// RemoteDynamicForward 处理SSH远程端口转发请求
// 这种处理器允许在服务器本机上打开端口，使得在具有严格AV保护的机器上，
// 可以使用普通SSH客户端通过`ssh -R port rssh.server`命令获得可代理的网络端口
//
// 参数:
//   - sshConn: SSH连接对象
//   - reqs: 接收SSH请求的通道
//   - log: 日志记录器
func RemoteDynamicForward(sshConn ssh.Conn, reqs <-chan *ssh.Request, log logger.Logger) {
	// 确保在函数结束时关闭SSH连接
	defer sshConn.Close()

	// 创建通道用于通知客户端已关闭
	clientClosed := make(chan bool)

	// 处理所有传入的请求
	for r := range reqs {
		switch r.Type {
		case "tcpip-forward":
			// 处理TCP/IP端口转发请求
			go func(req *ssh.Request) {
				var rf internal.RemoteForwardRequest

				// 解析请求负载
				err := ssh.Unmarshal(req.Payload, &rf)
				if err != nil {
					log.Warning("failed to unmarshal remote forward request: %s", err)
					req.Reply(false, []byte("Unable to open remote forward"))
					return
				}

				// 忽略rf.BindAddr，只使用指定端口，有助于缓解恶意客户端攻击
				l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", rf.BindPort))
				if err != nil {
					log.Warning("failed to listen for remote forward request: %s", err)
					req.Reply(false, []byte("Unable to open remote forward"))
					return
				}

				log.Info("Opened remote forward port on server: 127.0.0.1:%d", rf.BindPort)

				// 启动goroutine在客户端关闭时关闭监听器
				go func() {
					<-clientClosed
					l.Close()
				}()
				// 确保函数退出时关闭监听器
				defer l.Close()

				// 回复客户端请求成功
				req.Reply(true, nil)

				// 接受并处理传入的连接
				for {
					proxyCon, err := l.Accept()
					if err != nil {
						if !strings.Contains(err.Error(), "use of a closed") {
							log.Warning("failed to accept tcp connection: %s", err)
						}
						return
					}
					// 为每个连接启动goroutine处理数据
					go handleData(rf, proxyCon, sshConn)
				}

			}(r)

		default:
			// 处理未知请求类型
			log.Info("Client %s sent unknown proxy request type: %s", sshConn.RemoteAddr(), r.Type)

			if err := r.Reply(false, nil); err != nil {
				log.Info("Sending reply encountered an error: %s", err)
				sshConn.Close()
			}
		}
	}

	// 通知客户端已关闭
	clientClosed <- true

	log.Info("Proxy client %s ended", sshConn.RemoteAddr())
}

// handleData 处理远程端口转发的实际数据传输
// 参数:
//   - rf: 远程转发请求信息，包含目标地址和端口
//   - proxyCon: 本地代理连接(服务器上接受的连接)
//   - sshConn: SSH连接对象
//
// 返回值:
//   - error: 处理过程中遇到的错误
func handleData(rf internal.RemoteForwardRequest, proxyCon net.Conn, sshConn ssh.Conn) error {
	// 1. 解析原始连接地址和端口
	originatorAddress := proxyCon.LocalAddr().String()
	var originatorPort uint32

	// 从地址字符串中提取端口号
	for i := len(originatorAddress) - 1; i > 0; i-- {
		if originatorAddress[i] == ':' {
			// 转换端口号为整数
			e, err := strconv.ParseInt(originatorAddress[i+1:], 10, 32)
			if err != nil {
				sshConn.Close()
				return fmt.Errorf("failed to parse port number: %s", err)
			}

			originatorPort = uint32(e)
			originatorAddress = originatorAddress[:i] // 移除端口部分，保留IP地址
			break
		}
	}

	// 2. 构造SSH通道打开消息
	drtMsg := internal.ChannelOpenDirectMsg{
		Raddr: rf.BindAddr, // 远程目标地址
		Rport: rf.BindPort, // 远程目标端口

		Laddr: originatorAddress, // 本地(服务器)地址
		Lport: originatorPort,    // 本地(服务器)端口
	}

	// 3. 序列化消息并打开SSH通道
	b := ssh.Marshal(&drtMsg)
	destination, reqs, err := sshConn.OpenChannel("forwarded-tcpip", b)
	if err != nil {
		return err
	}

	// 4. 丢弃不需要的通道请求
	go ssh.DiscardRequests(reqs)

	// 5. 启动双向数据转发
	go func() {
		defer destination.Close()
		defer proxyCon.Close()
		// 从代理连接复制数据到SSH通道(服务器->客户端)
		io.Copy(destination, proxyCon)
	}()

	go func() {
		defer destination.Close()
		defer proxyCon.Close()
		// 从SSH通道复制数据到代理连接(客户端->服务器)
		io.Copy(proxyCon, destination)
	}()

	return nil
}
