package commands

import (
	"errors"
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal/server/users"          // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"              // 终端处理模块
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete" // 自动补全功能
	"github.com/QingYu-Su/Yui/pkg/logger"                     // 日志记录模块
)

// kill 结构体定义了终止客户端连接的命令类型
type kill struct {
	log logger.Logger // 日志记录器
}

// ValidArgs 方法返回 kill 命令的有效参数及其描述
func (k *kill) ValidArgs() map[string]string {
	return map[string]string{
		"y": "Do not prompt for confirmation before killing clients", // y参数: 不显示确认提示直接终止
	}
}

// Run 方法执行终止客户端连接的操作
// 参数:
//   - user: 当前用户对象
//   - tty: 终端输入输出接口
//   - line: 解析后的命令行参数
//
// 返回值: 执行过程中出现的错误
func (k *kill) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 检查参数数量是否正确(必须为1个)
	if len(line.Arguments) != 1 {
		return errors.New(k.Help(false)) // 参数错误时返回帮助信息
	}

	// 根据参数值查找匹配的客户端连接
	connections, err := user.SearchClients(line.Arguments[0].Value())
	if err != nil {
		return err
	}

	// 检查是否找到匹配的客户端
	if len(connections) == 0 {
		return fmt.Errorf("No clients matched '%s'", line.Arguments[0].Value())
	}

	// 如果没有设置-y参数，需要用户确认
	if !line.IsSet("y") {
		// 显示确认提示，包含匹配的客户端数量
		fmt.Fprintf(tty, "Kill %d clients? [N/y] ", len(connections))

		// 如果是终端设备，启用原始模式(直接读取单个字符)
		if term, ok := tty.(*terminal.Terminal); ok {
			term.EnableRaw()
		}

		// 读取单个字符作为用户确认
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

		// 检查用户输入是否为y/Y，否则中止执行
		if !(b[0] == 'y' || b[0] == 'Y') {
			return fmt.Errorf("\nUser did not enter y/Y, aborting")
		}

		fmt.Fprint(tty, "\n") // 输出换行符
	}

	// 终止匹配的客户端连接
	killedClients := 0
	for id, serverConn := range connections {
		// 向客户端发送kill请求
		serverConn.SendRequest("kill", false, nil)

		// 如果只匹配到一个客户端，返回特定格式的消息
		if len(connections) == 1 {
			return fmt.Errorf("%s killed", id)
		}
		killedClients++
	}

	// 返回终止的客户端数量
	return fmt.Errorf("%d connections killed", killedClients)
}

// Expect 方法返回自动补全的期望输入类型
func (k *kill) Expect(line terminal.ParsedLine) []string {
	// 如果参数数量<=1(即正在输入客户端ID时)，提供远程ID的自动补全
	if len(line.Arguments) <= 1 {
		return []string{autocomplete.RemoteId}
	}
	return nil // 其他情况不需要自动补全
}

// Help 方法返回kill命令的帮助信息
func (k *kill) Help(explain bool) string {
	if explain {
		return "Stop the execute of the rssh client." // 简要说明
	}

	// 完整帮助信息
	return terminal.MakeHelpText(
		k.ValidArgs(),                          // 有效参数列表
		"kill <remote_id>",                     // 基本用法
		"kill <glob pattern>",                  // 使用通配符匹配的用法
		"Stop the execute of the rssh client.", // 详细描述
	)
}

// Kill 函数是kill命令的构造函数
// 参数: log - 日志记录器
// 返回值: 初始化好的kill命令实例
func Kill(log logger.Logger) *kill {
	return &kill{
		log: log, // 初始化日志记录器
	}
}
