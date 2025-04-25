package connection

import (
	"fmt"
	"sync"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// Session 表示一个SSH会话，包含连接信息和状态
type Session struct {
	sync.RWMutex // 读写锁，用于并发控制

	// ServerConnection 是用户到服务器的SSH连接，用于创建新通道等操作
	// 注意：不应直接用于io.Copy操作
	ServerConnection ssh.Conn

	Pty *internal.PtyReq // 伪终端请求信息

	ShellRequests <-chan *ssh.Request // 接收shell请求的通道

	// SupportedRemoteForwards 记录用户请求的远程端口转发
	// 用于仅关闭用户特定的远程转发
	SupportedRemoteForwards map[internal.RemoteForwardRequest]bool // 使用map实现集合功能
}

// NewSession 创建一个新的Session实例
func NewSession(connection ssh.Conn) *Session {
	return &Session{
		ServerConnection:        connection,
		SupportedRemoteForwards: make(map[internal.RemoteForwardRequest]bool),
	}
}

// RegisterChannelCallbacks 注册通道类型回调处理器
// 参数:
//   - chans: 新通道的接收通道
//   - log: 日志记录器
//   - handlers: 通道类型到处理函数的映射
//
// 返回值:
//   - error: 当连接终止时返回错误
func RegisterChannelCallbacks(chans <-chan ssh.NewChannel, log logger.Logger, handlers map[string]func(newChannel ssh.NewChannel, log logger.Logger)) error {
	// 在goroutine中处理传入的通道
	for newChannel := range chans {
		t := newChannel.ChannelType()
		log.Info("正在处理通道: %s", t)

		// 检查是否有对应的处理器
		if callBack, ok := handlers[t]; ok {
			go callBack(newChannel, log) // 异步执行处理器
			continue
		}

		// 拒绝不支持的通道类型
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("不支持的通道类型: %s", t))
		log.Warning("接收到无效的通道类型 %q", t)
	}

	return fmt.Errorf("连接已终止")
}
