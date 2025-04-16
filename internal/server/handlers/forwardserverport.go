package handlers

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server/multiplexer"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// 全局变量定义
var (
	currentRemoteForwardsLck sync.RWMutex               // 读写锁，保护currentRemoteForwards和remoteForwards的并发访问
	currentRemoteForwards    = map[string]string{}      // 记录当前活跃的远程转发映射[监听地址]=>目标地址
	remoteForwards           = map[string]ssh.Channel{} // 缓存已建立的远程转发通道[地址]=>SSH通道
)

// chanAddress 实现net.Addr接口，表示通道的网络地址
type chanAddress struct {
	Port uint32 // 端口号
	IP   string // IP地址
}

// Network 返回网络类型标识
func (c *chanAddress) Network() string {
	return "remote_forward_tcp" // 固定返回远程转发TCP标识
}

// String 返回地址的字符串表示(IP:Port)
func (c *chanAddress) String() string {
	return net.JoinHostPort(c.IP, fmt.Sprintf("%d", c.Port))
}

// chanConn 实现net.Conn接口，包装SSH通道为网络连接
type chanConn struct {
	channel    ssh.Channel // 底层SSH通道
	localAddr  chanAddress // 本地地址信息
	remoteAddr chanAddress // 远程地址信息
}

// Read 从通道读取数据
func (c *chanConn) Read(b []byte) (n int, err error) {
	return c.channel.Read(b) // 直接调用底层通道的Read方法
}

// Write 向通道写入数据
func (c *chanConn) Write(b []byte) (n int, err error) {
	return c.channel.Write(b) // 直接调用底层通道的Write方法
}

// Close 关闭通道连接
func (c *chanConn) Close() error {
	return c.channel.Close() // 关闭底层SSH通道
}

// LocalAddr 返回本地地址信息
func (c *chanConn) LocalAddr() net.Addr {
	return &c.localAddr // 返回本地地址结构体指针
}

// RemoteAddr 返回远程地址信息
func (c *chanConn) RemoteAddr() net.Addr {
	return &c.remoteAddr // 返回远程地址结构体指针
}

// SetDeadline 设置读写截止时间(未实现)
func (c *chanConn) SetDeadline(t time.Time) error {
	return errors.New("not implemented on a channel")
}

// SetReadDeadline 设置读截止时间(未实现)
func (c *chanConn) SetReadDeadline(t time.Time) error {
	return errors.New("not implemented on a channel")
}

// SetWriteDeadline 设置写截止时间(未实现)
func (c *chanConn) SetWriteDeadline(t time.Time) error {
	return errors.New("not implemented on a channel")
}

// channelToConn 将SSH通道包装为标准的net.Conn接口
// 参数:
//   - channel: 要包装的SSH通道
//   - drtMsg: 包含地址和端口信息的通道打开消息
//
// 返回值:
//   - net.Conn: 实现了标准网络连接接口的对象
func channelToConn(channel ssh.Channel, drtMsg internal.ChannelOpenDirectMsg) net.Conn {
	return &chanConn{
		channel: channel,
		localAddr: chanAddress{
			Port: drtMsg.Lport, // 使用本地端口
			IP:   drtMsg.Raddr, // 使用远程地址作为本地地址
		},
		remoteAddr: chanAddress{
			Port: drtMsg.Rport, // 远程端口
			IP:   drtMsg.Raddr, // 远程地址
		},
	}
}

// ServerPortForward 创建处理服务器端口转发的ChannelHandler
// 参数:
//   - clientId: 客户端唯一标识
//
// 返回值:
//   - ChannelHandler: 处理SSH通道请求的函数
func ServerPortForward(clientId string) func(_ string, _ *users.User, newChannel ssh.NewChannel, log logger.Logger) {
	return func(_ string, _ *users.User, newChannel ssh.NewChannel, log logger.Logger) {
		// 1. 解析通道额外数据
		a := newChannel.ExtraData()

		var drtMsg internal.ChannelOpenDirectMsg
		err := ssh.Unmarshal(a, &drtMsg)
		if err != nil {
			log.Warning("Unable to unmarshal proxy %s", err)
			newChannel.Reject(ssh.ResourceShortage, "Unable to unmarshal proxy")
			return
		}

		// 2. 接受新通道
		connection, requests, err := newChannel.Accept()
		if err != nil {
			newChannel.Reject(ssh.ResourceShortage, "nope")
			log.Warning("Unable to accept new channel %s", err)
			return
		}

		// 3. 处理通道请求
		go func() {
			for req := range requests {
				if req.WantReply {
					req.Reply(false, nil) // 拒绝所有请求
				}
			}
			// 通道关闭时停止转发
			StopRemoteForward(clientId)
		}()

		// 4. 记录转发信息
		currentRemoteForwardsLck.Lock()
		remoteForwards[clientId] = connection
		currentRemoteForwards[clientId] = net.JoinHostPort(drtMsg.Raddr, fmt.Sprintf("%d", drtMsg.Rport))
		currentRemoteForwardsLck.Unlock()

		// 5. 将连接加入多路复用器
		multiplexer.ServerMultiplexer.QueueConn(channelToConn(connection, drtMsg))
	}
}

// StopRemoteForward 停止指定客户端的远程转发
// 参数:
//   - clientId: 要停止的客户端ID
func StopRemoteForward(clientId string) {
	currentRemoteForwardsLck.Lock()
	defer currentRemoteForwardsLck.Unlock()

	// 关闭通道并从映射中删除
	if remoteForwards[clientId] != nil {
		remoteForwards[clientId].Close()
	}

	delete(remoteForwards, clientId)
	delete(currentRemoteForwards, clientId)
}
