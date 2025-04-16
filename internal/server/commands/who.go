package commands

import (
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/internal/terminal"
)

// who 结构体定义了who命令的基础结构
// 该命令用于列出当前连接到RSSH服务器的所有用户
type who struct {
}

// ValidArgs 返回who命令支持的所有参数及其描述
// who命令不需要额外参数，返回空map
func (w *who) ValidArgs() map[string]string {
	return map[string]string{}
}

// Run 是who命令的主要执行方法
// 参数:
//   - user: 当前执行命令的用户(未使用)
//   - tty: 终端输入输出接口
//   - line: 解析后的命令行参数(未使用)
//
// 返回值: 执行过程中遇到的错误(总是返回nil)
func (w *who) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 获取当前所有连接的用户列表
	allUsers := users.ListUsers()

	// 遍历并打印每个用户信息
	for _, user := range allUsers {
		fmt.Fprintf(tty, "%s\n", user)
	}

	return nil
}

// Expect 提供命令的参数自动补全功能
// who命令不需要参数补全，返回nil
func (w *who) Expect(line terminal.ParsedLine) []string {
	return nil
}

// Help 返回命令的帮助信息
// 参数:
//   - explain: 是否只返回简短说明
//
// 返回值: 帮助信息字符串
func (w *who) Help(explain bool) string {
	const description = "List users connected to the RSSH server"
	if explain {
		return description // 简短说明
	}

	// 完整帮助信息，包含命令格式和功能描述
	return terminal.MakeHelpText(w.ValidArgs(),
		"who",       // 命令格式
		description) // 功能描述
}
