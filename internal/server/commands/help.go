package commands

import (
	"fmt"
	"io"
	"sort"

	"github.com/QingYu-Su/Yui/internal/server/users"          // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"              // 终端处理模块
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete" // 自动补全功能
	"github.com/QingYu-Su/Yui/pkg/table"                      // 表格输出工具
)

// help 结构体定义了帮助命令的类型
type help struct {
}

// ValidArgs 方法返回 help 命令的有效参数及其描述
func (h *help) ValidArgs() map[string]string {
	return map[string]string{
		"l": "List all function names only", // l参数: 仅列出所有命令名称
	}
}

// Run 方法执行帮助命令
// 参数:
//   - user: 当前用户对象(未使用)
//   - tty: 终端输入输出接口
//   - line: 解析后的命令行参数
//
// 返回值: 执行过程中出现的错误
func (h *help) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 如果设置了-l参数，仅列出所有命令名称
	if line.IsSet("l") {
		funcs := []string{}
		for funcName := range allCommands {
			funcs = append(funcs, funcName)
		}

		sort.Strings(funcs) // 按字母顺序排序

		// 输出所有命令名称
		for _, funcName := range funcs {
			fmt.Fprintln(tty, funcName)
		}

		return nil
	}

	// 如果没有提供参数，显示所有命令的简要帮助
	if len(line.Arguments) < 1 {
		// 创建表格输出，包含三列: 命令名称、功能和用途
		t, err := table.NewTable("Commands", "Function", "Purpose")
		if err != nil {
			return err
		}

		keys := []string{}
		for funcName := range allCommands {
			keys = append(keys, funcName)
		}

		sort.Strings(keys) // 按字母顺序排序

		// 将每个命令的简要帮助添加到表格中
		for _, k := range keys {
			hf := allCommands[k].Help
			err = t.AddValues(k, hf(true)) // hf(true)获取命令的简要说明
			if err != nil {
				return err
			}
		}

		t.Fprint(tty) // 输出表格到终端
		return nil
	}

	// 如果提供了具体命令名称，显示该命令的详细帮助
	l, ok := allCommands[line.Arguments[0].Value()]
	if !ok {
		return fmt.Errorf("Command %s not found", line.Arguments[0].Value())
	}

	// 输出命令描述
	fmt.Fprintf(tty, "\ndescription:\n%s\n", l.Help(true))
	// 输出命令完整用法
	fmt.Fprintf(tty, "\nusage:\n%s\n", l.Help(false))

	return nil
}

// Expect 方法返回自动补全的期望输入类型
func (h *help) Expect(line terminal.ParsedLine) []string {
	// 如果参数数量<=1(即正在输入命令名时)，提供命令名称的自动补全
	if len(line.Arguments) <= 1 {
		return []string{autocomplete.Functions}
	}
	return nil // 其他情况不需要自动补全
}

// Help 方法返回help命令自身的帮助信息
func (h *help) Help(explain bool) string {
	const description = "Get help for commands, or display all commands"
	if explain {
		return description // 简要说明
	}

	// 完整帮助信息
	return terminal.MakeHelpText(
		h.ValidArgs(),      // 有效参数列表
		"help",             // 基本用法
		"help <functions>", // 带参数用法示例
		description,        // 详细描述
	)
}
