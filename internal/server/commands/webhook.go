package commands

import (
	"errors"
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal/server/data"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/internal/terminal"
)

// webhook 结构体定义了webhook命令的基础结构
// 该命令用于管理服务器上的webhook配置
type webhook struct {
}

// ValidArgs 返回webhook命令支持的所有参数及其描述
// 返回值是一个map，其中key是参数名，value是参数描述
func (w *webhook) ValidArgs() map[string]string {
	return map[string]string{
		"on":       "Turns on webhook/s, must supply output as url", // 启用webhook，需要提供URL
		"off":      "Turns off existing webhook url",                // 禁用已有webhook
		"insecure": "Disable TLS certificate checking",              // 禁用TLS证书验证
		"l":        "Lists active webhooks",                         // 列出活跃webhook
	}
}

// Run 是webhook命令的主要执行方法
// 参数:
//   - user: 当前执行命令的用户
//   - tty: 终端输入输出接口
//   - line: 解析后的命令行参数
//
// 返回值: 执行过程中遇到的错误
func (w *webhook) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 如果没有提供任何参数，显示帮助信息
	if len(line.Flags) < 1 {
		fmt.Fprintf(tty, "%s", w.Help(false))
		return nil
	}

	// 处理列出webhook的逻辑 (-l 参数)
	if line.IsSet("l") {
		// 从数据库获取所有webhook配置
		webhooks, err := data.GetAllWebhooks()
		if err != nil {
			return err
		}

		// 如果没有活跃的webhook，显示提示信息
		if len(webhooks) == 0 {
			fmt.Fprintln(tty, "No active listeners")
			return nil
		}

		// 遍历并显示所有webhook URL
		for _, listener := range webhooks {
			fmt.Fprintf(tty, "%s\n", listener.URL)
		}
		return nil
	}

	// 检查是否同时设置了on和off参数
	on := line.IsSet("on")
	off := line.IsSet("off")

	// 如果同时设置了on和off，返回错误
	if on && off {
		return errors.New("cannot specify on and off at the same time")
	}

	// 处理启用webhook的逻辑 (-on 参数)
	if on {
		// 获取所有要启用的webhook URL
		addrs, err := line.GetArgsString("on")
		if err != nil {
			return err
		}

		// 遍历所有URL，逐个启用
		for i, addr := range addrs {
			// 创建webhook，根据insecure参数决定是否验证TLS证书
			resultingUrl, err := data.CreateWebhook(addr, !line.IsSet("insecure"))
			if err != nil {
				// 启用失败，显示错误信息
				fmt.Fprintf(tty, "(%d/%d) Failed: %s, reason: %s\n", i+1, len(addrs), resultingUrl, err.Error())
				continue
			}

			// 启用成功，显示成功信息
			fmt.Fprintf(tty, "(%d/%d) Enabled webhook: %s\n", i+1, len(addrs), resultingUrl)
		}

		return nil
	}

	// 处理禁用webhook的逻辑 (-off 参数)
	if off {
		// 获取所有要禁用的webhook URL
		existingWebhooks, err := line.GetArgsString("off")
		if err != nil {
			return err
		}

		// 遍历所有URL，逐个禁用
		for i, hook := range existingWebhooks {
			// 删除webhook
			err := data.DeleteWebhook(hook)
			if err != nil {
				// 禁用失败，显示错误信息
				fmt.Fprintf(tty, "(%d/%d) Failed to remove: %s, reason: %s\n", i+1, len(existingWebhooks), hook, err.Error())
				continue
			}

			// 禁用成功，显示成功信息
			fmt.Fprintf(tty, "(%d/%d) Disabled webhook: %s\n", i+1, len(existingWebhooks), hook)
		}
		return nil
	}

	return nil
}

// Expect 提供命令的参数自动补全功能
// 当前未实现，返回nil表示不提供自动补全
func (w *webhook) Expect(line terminal.ParsedLine) []string {
	return nil
}

// Help 返回命令的帮助信息
// 参数:
//   - explain: 是否只返回简短说明
//
// 返回值: 帮助信息字符串
func (w *webhook) Help(explain bool) string {
	if explain {
		return "Add or remove webhooks" // 简短说明
	}

	// 完整帮助信息，包含参数说明和使用示例
	return terminal.MakeHelpText(w.ValidArgs(),
		"webhook [OPTIONS]", // 命令格式
		"Allows you to set webhooks which currently show the joining and leaving of clients", // 功能描述
	)
}
