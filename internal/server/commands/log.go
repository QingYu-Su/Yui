package commands

import (
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal/server/users"          // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"              // 终端处理模块
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete" // 自动补全功能
	"github.com/QingYu-Su/Yui/pkg/logger"                     // 日志记录模块
	"golang.org/x/crypto/ssh"                                 // SSH协议库
)

// logCommand 结构体定义了日志收集命令的类型
type logCommand struct {
}

// ValidArgs 方法返回 logCommand 命令的有效参数及其描述
func (l *logCommand) ValidArgs() map[string]string {
	return map[string]string{
		"c":          "client to collect logging from",                                                                                                    // 指定要收集日志的客户端
		"log-level":  "Set client log level, default for generated clients is currently: " + fmt.Sprintf("%q", logger.UrgencyToStr(logger.GetLogLevel())), // 设置客户端日志级别
		"to-file":    "direct output to file, takes a path as an argument",                                                                                // 将日志输出到文件
		"to-console": "directs output to the server console (or current connection), stop with any keypress",                                              // 将日志输出到控制台
	}
}

// Run 方法执行日志收集命令
func (l *logCommand) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 检查是否指定了客户端
	if !line.IsSet("c") {
		fmt.Fprintln(tty, "missing client -c")
		return nil
	}

	// 获取客户端标识符
	client, err := line.GetArgString("c")
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}

	// 获取客户端连接
	connection, err := user.GetClient(client)
	if err != nil {
		return err
	}

	// 处理日志级别设置
	logLevel, err := line.GetArgString("log-level")
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	} else {
		// 验证日志级别有效性
		_, err := logger.StrToUrgency(logLevel)
		if err != nil {
			return fmt.Errorf("invalid log level %q", logLevel)
		}

		// 向客户端发送日志级别设置请求
		_, _, err = connection.SendRequest("log-level", false, []byte(logLevel))
		if err != nil {
			return fmt.Errorf("failed to send log level request to client (may be outdated): %s", err)
		}
	}

	// 处理控制台日志输出
	if line.IsSet("to-console") {
		// 如果是终端设备，启用原始模式
		term, isTerm := tty.(*terminal.Terminal)
		if isTerm {
			term.EnableRaw()
		}

		// 打开日志输出通道
		consoleLog, reqs, err := connection.OpenChannel("log-to-console", nil)
		if err != nil {
			return fmt.Errorf("client would not open log to console channel (maybe wrong version): %s", err)
		}

		// 丢弃不需要的请求
		go ssh.DiscardRequests(reqs)

		// 启动goroutine监听按键停止
		go func() {
			b := make([]byte, 1)
			tty.Read(b)
			consoleLog.Close()
		}()

		// 读取并输出日志数据
		for {
			buff := make([]byte, 1024)
			n, err := consoleLog.Read(buff)
			if err != nil {
				break
			}

			fmt.Fprintf(tty, "%s\r", buff[:n])
		}

		// 如果是终端设备，禁用原始模式
		if isTerm {
			term.DisableRaw()
		}

	} else if line.IsSet("to-file") {
		// 处理文件日志输出
		filepath, err := line.GetArgString("to-file")
		if err != nil && err != terminal.ErrFlagNotSet {
			return err
		}

		// 向客户端发送日志到文件请求
		_, _, err = connection.SendRequest("log-to-file", false, []byte(filepath))
		if err != nil {
			return fmt.Errorf("failed to send request to client: %s", err)
		}
		fmt.Fprintln(tty, "log to file request sent to client!")
	}

	return nil
}

// Expect 方法返回自动补全的期望输入类型
func (l *logCommand) Expect(line terminal.ParsedLine) []string {
	// 如果当前输入的是客户端部分，提供远程ID的自动补全
	if line.Section != nil {
		switch line.Section.Value() {
		case "c":
			return []string{autocomplete.RemoteId}
		}
	}

	return nil
}

// Help 方法返回log命令的帮助信息
func (l *logCommand) Help(explain bool) string {
	const description = "Collect log output from client"
	if explain {
		return description // 简要说明
	}

	// 完整帮助信息
	return terminal.MakeHelpText(
		l.ValidArgs(),               // 有效参数列表
		"log [OPTIONS] <remote_id>", // 使用语法
		description,                 // 详细描述
	)
}

// Log 函数是log命令的构造函数
func Log(log logger.Logger) *logCommand {
	return &logCommand{}
}
