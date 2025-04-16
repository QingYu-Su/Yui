package commands

import (
	"io" // 提供基本I/O接口

	"github.com/QingYu-Su/Yui/internal/server/users" // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"     // 终端交互模块
)

// clear 结构体实现清屏功能
type clear struct {
}

// ValidArgs 定义命令支持的参数（clear命令无需参数）
func (e *clear) ValidArgs() map[string]string {
	return map[string]string{} // 返回空map表示不接受任何参数
}

// Run 是clear命令的主要执行逻辑
func (e *clear) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 尝试将tty转换为Terminal类型
	term, ok := tty.(*terminal.Terminal)
	if !ok {
		// 如果不是终端设备，直接返回（不做任何操作）
		return nil
	}

	// 调用Terminal的Clear方法清屏
	term.Clear()

	return nil // 返回nil表示执行成功
}

// Expect 实现命令的自动补全逻辑（clear命令无需自动补全）
func (e *clear) Expect(line terminal.ParsedLine) []string {
	return nil // 返回nil表示不提供自动补全
}

// Help 提供命令的帮助信息
func (e *clear) Help(explain bool) string {
	const description = "Clear server console" // 命令功能描述

	if explain {
		// 简要说明模式
		return description
	}

	// 完整帮助信息模式
	return terminal.MakeHelpText(
		e.ValidArgs(), // 获取参数说明（空）
		"clear",       // 命令使用示例
		description,   // 功能描述
	)
}
