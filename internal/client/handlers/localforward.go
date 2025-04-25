package handlers

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// LocalForward 处理SSH本地端口转发请求
// 参数:
//
//	newChannel - 新SSH通道请求
//	l - 日志记录器
func LocalForward(newChannel ssh.NewChannel, l logger.Logger) {
	// 1. 获取通道附加数据(包含转发目标信息)
	a := newChannel.ExtraData()

	// 2. 解析转发目标信息(RFC 4254 7.2)
	var drtMsg internal.ChannelOpenDirectMsg
	err := ssh.Unmarshal(a, &drtMsg)
	if err != nil {
		l.Warning("无法解析转发目标: %s", err)
		newChannel.Reject(ssh.ResourceShortage, "无法解析转发目标")
		return
	}

	// 3. 创建带超时的拨号器(5秒超时)
	d := net.Dialer{Timeout: 5 * time.Second}

	// 4. 构造目标地址字符串(IP:PORT)
	dest := net.JoinHostPort(drtMsg.Raddr, fmt.Sprintf("%d", drtMsg.Rport))

	// 5. 建立到目标服务的TCP连接
	tcpConn, err := d.Dial("tcp", dest)
	if err != nil {
		l.Warning("无法连接到目标服务: %s", err)
		newChannel.Reject(ssh.ConnectionFailed, "无法连接到 "+dest)
		return
	}
	defer tcpConn.Close() // 确保最终关闭连接

	// 6. 取消后续操作的超时限制
	d.Timeout = 0

	// 7. 接受SSH通道请求
	connection, requests, err := newChannel.Accept()
	if err != nil {
		newChannel.Reject(ssh.ResourceShortage, dest)
		l.Warning("无法接受新通道: %s", err)
		return
	}
	defer connection.Close() // 确保最终关闭通道

	// 8. 丢弃不需要的通道请求
	go ssh.DiscardRequests(requests)

	// 9. 启动goroutine处理目标服务→SSH客户端的数据转发
	go func() {
		defer tcpConn.Close()
		defer connection.Close()
		io.Copy(connection, tcpConn) // 阻塞式复制数据
	}()

	// 10. 处理SSH客户端→目标服务的数据转发(主goroutine)
	io.Copy(tcpConn, connection) // 阻塞式复制数据
}
