package handlers

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// 处理SSH客户端的本地端口转发数据通道，并将其数据转发到RSSH客户端上的jump（自定义）通道上
func LocalForward(_ string, user *users.User, newChannel ssh.NewChannel, log logger.Logger) {
	// 1. 解析转发目标信息
	proxyTarget := newChannel.ExtraData() // 获取通道额外数据

	var drtMsg internal.ChannelOpenDirectMsg
	err := ssh.Unmarshal(proxyTarget, &drtMsg) // 反序列化为目标消息结构
	if err != nil {
		log.Warning("Unable to unmarshal proxy destination: %s", err)
		return
	}

	// 2. 处理特殊IP地址转换(兼容旧版客户端)
	addr := net.ParseIP(drtMsg.Raddr)
	if addr != nil {
		// 将IP地址转换回原始ID值
		value := int64(binary.BigEndian.Uint32(addr))
		if len(addr) == 16 { // IPv6情况处理
			value = int64(binary.BigEndian.Uint32(addr[12:16]))
		}
		drtMsg.Raddr = strconv.FormatInt(value, 10) // 转换回字符串ID
	}

	// 3. 查找匹配的目标客户端
	foundClients, err := user.SearchClients(drtMsg.Raddr)
	if err != nil {
		newChannel.Reject(ssh.Prohibited, err.Error()) // 拒绝通道并返回错误
		return
	}

	// 4. 检查客户端匹配结果
	if len(foundClients) == 0 {
		newChannel.Reject(ssh.ConnectionFailed,
			fmt.Sprintf("\n\nNo clients matched '%s'\n", drtMsg.Raddr))
		return
	}

	if len(foundClients) > 1 {
		newChannel.Reject(ssh.ConnectionFailed,
			fmt.Sprintf("\n\n'%s' matches multiple clients please choose a more specific identifier\n",
				drtMsg.Raddr))
		return
	}

	// 5. 获取目标客户端连接(取map中第一个元素)
	var target ssh.Conn
	for k := range foundClients {
		target = foundClients[k]
		break
	}

	// 6. 打开目标通道
	targetConnection, targetRequests, err := target.OpenChannel("jump", nil)
	if err != nil {
		newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	defer targetConnection.Close()         // 确保关闭连接
	go ssh.DiscardRequests(targetRequests) // 丢弃不需要的请求

	// 7. 接受客户端通道
	connection, requests, err := newChannel.Accept()
	if err != nil {
		newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	defer connection.Close()
	go ssh.DiscardRequests(requests)

	// 8. 建立双向数据转发
	go func() {
		io.Copy(connection, targetConnection) // RSSH客户端->SSH客户端
		connection.Close()
	}()
	io.Copy(targetConnection, connection) // SSH客户端->RSSH客户端
}
