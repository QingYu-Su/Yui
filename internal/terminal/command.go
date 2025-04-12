package terminal

import (
	"io"

	"github.com/QingYu-Su/Yui/internal/server/users"
)

// Command 接口定义了终端命令的基本行为，由外部实现
type Command interface {
	// Expect 返回命令的预期语法，用于自动补全过程
	// 参数:
	//   line - 已解析的命令行
	// 返回值:
	//   字符串切片，表示可能的自动补全选项
	Expect(line ParsedLine) []string

	// Run 执行命令
	// 参数:
	//   user - 执行命令的用户对象
	//   output - 用于命令输出的读写接口
	//   line - 已解析的命令行
	// 返回值:
	//   错误对象，表示执行过程中是否出错
	Run(user *users.User, output io.ReadWriter, line ParsedLine) error

	// Help 返回命令的帮助文本
	// 参数:
	//   explain - 是否返回详细解释
	// 返回值:
	//   帮助文本字符串
	Help(explain bool) string

	// ValidArgs 返回命令的有效参数和它们的描述
	// 返回值:
	//   映射表，键为参数名，值为参数描述
	//   可用于生成帮助文本
	ValidArgs() map[string]string
}
