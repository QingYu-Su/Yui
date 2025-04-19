package handlers

import (
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// 定义一个SSH通道处理函数，用于处理SSH通道消息
// 输入参数包括：SSH客户端信息（用户名@地址），用户（服务端用户），SSH通道，日志器
type ChannelHandler func(connectionDetails string, user *users.User, newChannel ssh.NewChannel, log logger.Logger)
