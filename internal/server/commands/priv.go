package commands

import (
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal/server/users" // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"     // 终端处理模块
)

// privilege 结构体定义了权限查看命令的类型
type privilege struct {
}

// ValidArgs 方法返回 privilege 命令的有效参数及其描述
// 该命令不需要参数，返回空map
func (p *privilege) ValidArgs() map[string]string {
	return map[string]string{}
}

// Run 方法执行权限查看操作
// 参数:
//   - user: 当前用户对象
//   - tty: 终端输入输出接口
//   - line: 解析后的命令行参数(未使用)
//
// 返回值: 执行过程中出现的错误(总是返回nil)
func (p *privilege) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 输出当前用户的权限级别字符串
	fmt.Fprintf(tty, "%s\n", user.PrivilegeString())
	return nil
}

// Expect 方法返回自动补全的期望输入类型
// 该命令不需要自动补全，返回nil
func (p *privilege) Expect(line terminal.ParsedLine) []string {
	return nil
}

// Help 方法返回privilege命令的帮助信息
func (p *privilege) Help(explain bool) string {
	if explain {
		return "Privilege shows the current user privilege level." // 简要说明
	}

	// 完整帮助信息
	return terminal.MakeHelpText(
		p.ValidArgs(), // 空参数列表
		"priv ",       // 使用语法
		"Print the currrent user privilege level.", // 详细描述
	)
}
