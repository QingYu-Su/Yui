package commands

import (
	"github.com/QingYu-Su/Yui/internal/server/users" // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"     // 终端交互模块
	"github.com/QingYu-Su/Yui/pkg/logger"            // 日志记录模块
)

// 全局命令映射表，用于帮助系统生成漂亮的表格
// 理想情况下可以通过自动注册机制来管理这些命令
var allCommands = map[string]terminal.Command{
	"ls":           &list{},              // 列出资源
	"help":         &help{},              // 帮助命令
	"kill":         &kill{},              // 终止进程
	"connect":      &connect{},           // 连接服务
	"exit":         &exit{},              // 退出系统
	"link":         &link{},              // 生成客户端链接
	"exec":         &exec{},              // 执行命令
	"who":          &who{},               // 查看用户信息
	"watch":        &watch{},             // 监控变化
	"listen":       &listen{},            // 监听端口
	"webhook":      &webhook{},           // Webhook管理
	"version":      &version{},           // 版本信息
	"priv":         &privilege{},         // 权限管理
	"access":       &access{},            // 访问控制
	"autocomplete": &shellAutocomplete{}, // 自动补全
	"log":          &logCommand{},        // 日志管理
	"clear":        &clear{},             // 清屏
}

// CreateCommands 创建特定于某个用户和SSH客户端的RSSH服务端命令集合，主要是用于在SSH客户端会话通道中执行命令
func CreateCommands(session string, user *users.User, log logger.Logger, datadir string) map[string]terminal.Command {
	// 初始化命令集合，部分命令需要依赖注入
	var o = map[string]terminal.Command{
		"ls":           &list{}, // 简单命令直接实例化
		"help":         &help{},
		"kill":         Kill(log),                   // 需要日志依赖的命令
		"connect":      Connect(session, user, log), // 需要会话和用户信息的命令
		"exit":         &exit{},
		"link":         &link{},
		"exec":         &exec{},
		"who":          &who{},
		"watch":        Watch(datadir), // 需要数据目录的命令
		"listen":       Listen(log),    // 需要日志记录的命令
		"webhook":      &webhook{},
		"version":      &version{},
		"priv":         &privilege{},
		"access":       &access{},
		"autocomplete": &shellAutocomplete{},
		"log":          Log(log), // 日志相关命令
		"clear":        &clear{},
	}

	return o
}

// addDuplicateFlags 为命令添加多个相同含义的标志(flag)别名
// 参数:
//   - helpText: 标志的帮助文本
//   - m: 目标映射表
//   - flags: 要添加的标志名称列表
func addDuplicateFlags(helpText string, m map[string]string, flags ...string) {
	// 为每个标志名称添加相同的帮助文本
	for _, flag := range flags {
		m[flag] = helpText
	}
}
