package commands

import (
	"errors"
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/internal/terminal"
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete"
)

// access 结构体实现客户端访问权限管理功能
type access struct {
}

// Run 方法是 access 命令的主要执行逻辑
func (s *access) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	var err error

	// 获取客户端匹配模式（支持 -p 或 --pattern 参数）
	pattern, err := line.GetArgString("p")
	if err != nil {
		if err != terminal.ErrFlagNotSet {
			return err
		}
		pattern, err = line.GetArgString("pattern")
		if err != nil && err != terminal.ErrFlagNotSet {
			return err
		}
	}

	// 获取新所有者设置（支持 -o 或 --owners 参数）
	newOwners, err := line.GetArgString("o")
	if err != nil {
		if err != terminal.ErrFlagNotSet {
			return err
		}
		newOwners, err = line.GetArgString("owners")
		if err != nil && err != terminal.ErrFlagNotSet {
			return err
		}
	}

	// 处理特殊标志：-c/--current 表示设置为当前用户
	if line.IsSet("c") || line.IsSet("current") {
		newOwners = user.Username()
	}

	// 处理特殊标志：-a/--all 表示设置为空（所有用户可访问）
	if line.IsSet("a") || line.IsSet("all") {
		newOwners = ""
	}

	// 验证所有者格式（不能包含空格）
	if spaceMatcher.MatchString(newOwners) {
		return errors.New("new owners cannot contain spaces")
	}

	// 搜索匹配的客户端连接
	connections, err := user.SearchClients(pattern)
	if err != nil {
		return err
	}

	// 检查是否有匹配的客户端
	if len(connections) == 0 {
		return fmt.Errorf("No clients matched '%s'", pattern)
	}

	// 确认操作（除非设置了 -y 跳过确认）
	if !line.IsSet("y") {
		// 显示确认提示
		fmt.Fprintf(tty, "Modifing ownership of %d clients? [N/y] ", len(connections))

		// 如果是终端设备，启用原始模式以获取单个字符输入
		if term, ok := tty.(*terminal.Terminal); ok {
			term.EnableRaw()
		}

		// 读取用户输入
		b := make([]byte, 1)
		_, err := tty.Read(b)
		if err != nil {
			if term, ok := tty.(*terminal.Terminal); ok {
				term.DisableRaw()
			}
			return err
		}
		if term, ok := tty.(*terminal.Terminal); ok {
			term.DisableRaw()
		}

		// 检查用户确认（必须输入 y/Y）
		if !(b[0] == 'y' || b[0] == 'Y') {
			return fmt.Errorf("\nUser did not enter y/Y, aborting")
		}
	}

	// 执行所有权变更
	changes := 0
	for id := range connections {
		err := user.SetOwnership(id, newOwners)
		if err != nil {
			fmt.Fprintf(tty, "error changing ownership of %s: err %s", id, err)
			continue
		}
		changes++
	}

	// 返回变更统计结果
	return fmt.Errorf("%d client owners modified", changes)
}

// ValidArgs 定义命令支持的参数及其说明
func (s *access) ValidArgs() map[string]string {
	// 初始化参数映射表
	r := map[string]string{
		"y": "Auto confirm prompt", // -y 自动确认提示
	}

	// 使用辅助函数添加参数别名（相同的描述信息）

	// 客户端匹配模式参数（-p/--pattern）
	addDuplicateFlags("Clients to act on", r, "p", "pattern")

	// 所有权设置参数（-o/--owners）
	addDuplicateFlags("Set the ownership of the client, comma seperated user list", r, "o", "owners")

	// 设置为当前用户参数（-c/--current）
	addDuplicateFlags("Set the ownership to only the current user", r, "c", "current")

	// 设置为所有用户可访问参数（-a/--all）
	addDuplicateFlags("Set the ownership to anyone on the server", r, "a", "all")

	return r
}

// Expect 实现命令的自动补全逻辑
func (s *access) Expect(line terminal.ParsedLine) []string {
	// 检查是否有命令片段（子命令/参数）
	if line.Section != nil {
		// 根据参数类型返回不同的自动补全建议
		switch line.Section.Value() {
		case "p", "pattern": // 当输入 -p 或 --pattern 时
			return []string{autocomplete.RemoteId} // 返回远程ID的自动补全建议
		}
	}
	return nil // 默认不提供自动补全
}

// Help 提供命令的帮助信息
func (s *access) Help(explain bool) string {
	// 简单说明模式
	if explain {
		return "Temporarily share/unhide client connection."
	}

	// 完整帮助信息模式
	return terminal.MakeHelpText(
		s.ValidArgs(),                  // 获取参数说明
		"access [OPTIONS] -p <FILTER>", // 命令使用示例
		"Change ownership of client connection, only lasts until restart of rssh server, to make permanent edit authorized_controllee_keys 'owner' option", // 功能描述
		"Filter uses glob matching against all attributes of a target (id, public key hash, hostname, ip)",                                                 // 额外说明
	)
}
