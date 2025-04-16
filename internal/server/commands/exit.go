// 包 commands 包含服务器命令的实现
package commands

import (
	"io"

	"github.com/QingYu-Su/Yui/internal/server/users" // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"     // 终端处理模块
)

// exit 结构体定义了退出命令的类型
type exit struct {
}

// ValidArgs 方法返回 exit 命令的有效参数及其描述
// exit 命令不接受任何参数，所以返回空map
func (e *exit) ValidArgs() map[string]string {
	return map[string]string{}
}

// Run 方法执行退出命令
// 参数:
//   - user: 当前用户对象(未使用)
//   - tty: 终端输入输出接口(未使用)
//   - line: 解析后的命令行参数(未使用)
//
// 返回值: 返回 io.EOF 错误表示连接结束
func (e *exit) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	return io.EOF // 返回EOF错误表示需要关闭连接
}

// Expect 方法返回自动补全的期望输入类型
// exit 命令不需要自动补全，所以返回nil
func (e *exit) Expect(line terminal.ParsedLine) []string {
	return nil
}

// Help 方法返回命令的帮助信息
// 参数: explain - 是否只需要简要说明
// 返回值: 帮助信息字符串
func (e *exit) Help(explain bool) string {
	const description = "Close server console connection" // 命令详细描述

	if explain {
		return "Close server console" // 简要说明
	}

	// 完整帮助信息，包括参数说明(无)和使用示例
	return terminal.MakeHelpText(
		e.ValidArgs(), // 空参数列表
		"exit",        // 命令使用格式
		description,   // 详细说明
	)
}
