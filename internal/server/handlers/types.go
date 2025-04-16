package handlers

import (
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// ChannelHandler 定义了一个SSH通道处理函数的类型签名
// 该接口用于处理不同类型的SSH通道请求
//
// 参数说明:
//   - connectionDetails: 连接详情字符串，包含客户端连接信息
//   - user: 表示已认证的用户对象，包含用户相关信息
//   - newChannel: SSH新通道请求对象，包含通道类型和配置
//   - log: 日志记录器实例，用于记录处理过程中的日志
//
// 使用场景:
// 这个handler类型用于注册到SSH服务器，当客户端请求打开新通道时，
// 服务器会根据通道类型调用对应的ChannelHandler进行处理
type ChannelHandler func(connectionDetails string, user *users.User, newChannel ssh.NewChannel, log logger.Logger)
