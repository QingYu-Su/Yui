package commands

import (
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/internal/terminal"
)

// version 结构体定义了一个版本命令
type version struct {
}

// ValidArgs 返回命令的有效参数映射
// 对于version命令来说，没有参数需要验证，所以返回空map
func (v *version) ValidArgs() map[string]string {
	return map[string]string{}
}

// Run 是version命令的执行函数
// 它向终端输出服务器的构建版本信息
func (v *version) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 将版本信息写入终端
	fmt.Fprintln(tty, internal.Version)
	return nil
}

// Expect 返回命令期望的参数列表
// 对于version命令来说，不需要任何参数，所以返回nil
func (v *version) Expect(line terminal.ParsedLine) []string {
	return nil
}

// Help 返回命令的帮助信息
// 当explain为true时返回简短的描述
// 否则返回格式化的完整帮助文本
func (v *version) Help(explain bool) string {
	// 命令描述
	const description = "Give server build version"

	if explain {
		return description
	}

	// 使用terminal包中的MakeHelpText函数生成格式化的帮助文本
	return terminal.MakeHelpText(v.ValidArgs(),
		"version",
		description)
}
