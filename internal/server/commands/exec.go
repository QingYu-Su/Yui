// 包 commands 包含服务器命令的实现
package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/QingYu-Su/Yui/internal/server/users"          // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"              // 终端处理模块
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete" // 自动补全功能
	"golang.org/x/crypto/ssh"                                 // SSH协议库
)

// exec 结构体定义了一个执行命令的类型
type exec struct {
}

// ValidArgs 方法返回 exec 命令的有效参数及其描述
// 返回值是一个映射，键是参数名，值是对参数的描述
func (e *exec) ValidArgs() map[string]string {
	return map[string]string{
		"q":   "Quiet, no output (will also remove confirmation prompt)",   // q参数: 静默模式，无输出(同时移除确认提示)
		"y":   "No confirmation prompt",                                    // y参数: 不显示确认提示
		"raw": "Do not label output blocks with the client they came from", // raw参数: 不在输出块中标记来自哪个客户端
	}
}

// Run 方法执行远程命令
// 参数:
//   - user: 当前用户对象
//   - tty: 终端输入输出接口
//   - line: 解析后的命令行参数
//
// 返回值: 执行过程中出现的错误
func (e *exec) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 检查参数数量是否足够(至少需要主机/过滤器和命令两个参数)
	if len(line.Arguments) < 2 {
		return fmt.Errorf("Not enough arguments supplied. Needs at least, host|filter command...")
	}

	// 初始化过滤器和命令字符串
	filter := ""
	command := ""
	if len(line.ArgumentsAsStrings()) > 0 {
		filter = line.ArgumentsAsStrings()[0]            // 第一个参数作为主机过滤器
		command = line.RawLine[line.Arguments[0].End():] // 剩余部分作为要执行的命令
	}

	command = strings.TrimSpace(command) // 去除命令前后的空白字符

	// 根据过滤器查找匹配的客户端
	matchingClients, err := user.SearchClients(filter)
	if err != nil {
		return err
	}

	// 检查是否找到匹配的客户端
	if len(matchingClients) == 0 {
		return fmt.Errorf("Unable to find match for '" + filter + "'\n")
	}

	// 如果不是静默模式(q)也不是原始输出模式(raw)，则显示确认提示
	if !(line.IsSet("q") || line.IsSet("raw")) {
		// 如果没有设置自动确认(y)，则等待用户输入确认
		if !line.IsSet("y") {
			fmt.Fprintf(tty, "Run command? [N/y] ") // 显示确认提示

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
		}
	}

	// 准备SSH命令请求结构体
	var c struct {
		Cmd string
	}
	c.Cmd = command

	// 将命令结构体序列化为SSH协议格式
	commandByte := ssh.Marshal(&c)

	// 遍历所有匹配的客户端执行命令
	for id, client := range matchingClients {
		// 如果不是静默模式也不是原始输出模式，显示客户端信息
		if !(line.IsSet("q") || line.IsSet("raw")) {
			fmt.Fprint(tty, "\n\n")
			fmt.Fprintf(tty, "%s (%s) output:\n", id, client.User()+"@"+client.RemoteAddr().String())
		}

		// 打开SSH会话通道
		newChan, r, err := client.OpenChannel("session", nil)
		if err != nil && !line.IsSet("q") {
			fmt.Fprintf(tty, "Failed: %s\n", err)
			continue
		}
		go ssh.DiscardRequests(r) // 丢弃不需要的请求

		// 发送执行命令请求
		response, err := newChan.SendRequest("exec", true, commandByte)
		if err != nil && !line.IsSet("q") {
			fmt.Fprintf(tty, "Failed: %s\n", err)
			continue
		}

		// 检查客户端是否拒绝执行命令
		if !response && !line.IsSet("q") {
			fmt.Fprintf(tty, "Failed: client refused\n")
			continue
		}

		// 如果是静默模式，丢弃所有输出
		if line.IsSet("q") {
			io.Copy(io.Discard, newChan)
			continue
		}

		// 将命令输出复制到终端
		io.Copy(tty, newChan)
		newChan.Close() // 关闭通道
	}

	fmt.Fprint(tty, "\n") // 输出换行符

	return nil
}

// Expect 方法返回自动补全的期望输入类型
// 参数: line - 解析后的命令行参数
// 返回值: 期望的自动补全类型列表(这里只期望远程主机ID)
func (e *exec) Expect(line terminal.ParsedLine) []string {
	return []string{autocomplete.RemoteId} // 返回远程ID的自动补全类型
}

// Help 方法返回命令的帮助信息
// 参数: explain - 是否只需要简要说明
// 返回值: 帮助信息字符串
func (e *exec) Help(explain bool) string {
	// 如果只需要简要说明，返回一句话描述
	if explain {
		return "Execute a command on one or more rssh client"
	}

	// 否则返回完整的帮助信息，包括参数说明和使用示例
	return terminal.MakeHelpText(
		e.ValidArgs(),                        // 有效的参数列表
		"exec [OPTIONS] filter|host command", // 命令使用格式
		"Filter uses glob matching against all attributes of a target (hostname, ip, id), allowing you to run a command against multiple machines", // 详细说明
	)
}
