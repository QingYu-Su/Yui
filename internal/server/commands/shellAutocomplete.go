package commands

import (
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/QingYu-Su/Yui/internal/server/users" // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"     // 终端处理模块
)

// shellAutocomplete 结构体定义了自动补全命令的类型
type shellAutocomplete struct {
}

// completion 常量定义了Bash/Zsh自动补全脚本模板
const completion = `
_RSSHCLIENTSCOMPLETION()
{
    local cur=${COMP_WORDS[COMP_CWORD]}
    COMPREPLY=( $(compgen -W "$(ssh REPLACEMEWITH_THE_REAL_SERVER_NAME_4259e892-f7ca-4428-afb0-9af135ce9458 autocomplete --clients)" -- $cur) )
}

_RSSHFUNCTIONSCOMPLETIONS()
{
    local cur=${COMP_WORDS[COMP_CWORD]}
    COMPREPLY=( $(compgen -W "$(ssh REPLACEMEWITH_THE_REAL_SERVER_NAME_4259e892-f7ca-4428-afb0-9af135ce9458 help -l)" -- $cur) )
}

complete -F _RSSHFUNCTIONSCOMPLETIONS ssh REPLACEMEWITH_THE_REAL_SERVER_NAME_4259e892-f7ca-4428-afb0-9af135ce9458 

complete -F _RSSHCLIENTSCOMPLETION ssh -J REPLACEMEWITH_JUMPHOST_THE_REAL_SERVER_NAME_6e020f45-6d31-4c98-af4d-0ba75b48b664

complete -F _RSSHCLIENTSCOMPLETION ssh REPLACEMEWITH_THE_REAL_SERVER_NAME_4259e892-f7ca-4428-afb0-9af135ce9458 exec 
complete -F _RSSHCLIENTSCOMPLETION ssh REPLACEMEWITH_THE_REAL_SERVER_NAME_4259e892-f7ca-4428-afb0-9af135ce9458 connect 
complete -F _RSSHCLIENTSCOMPLETION ssh REPLACEMEWITH_THE_REAL_SERVER_NAME_4259e892-f7ca-4428-afb0-9af135ce9458 listen -c 
complete -F _RSSHCLIENTSCOMPLETION ssh REPLACEMEWITH_THE_REAL_SERVER_NAME_4259e892-f7ca-4428-afb0-9af135ce9458 kill `

// ValidArgs 方法返回 shellAutocomplete 命令的有效参数及其描述
func (k *shellAutocomplete) ValidArgs() map[string]string {
	return map[string]string{
		"clients":          "Return a list of client ids",                                                                                           // 返回客户端ID列表
		"shell-completion": "Generate bash completion to put in .bashrc/.zshrc with optional server name (will use rssh as server name if not set)", // 生成shell自动补全脚本
	}
}

// Run 方法执行自动补全命令
func (k *shellAutocomplete) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 处理--clients参数，返回客户端列表
	if line.IsSet("clients") {
		clients, err := user.SearchClients("")
		if err != nil {
			return nil
		}

		// 输出每个客户端的详细信息
		for id, conn := range clients {
			keyId := conn.Permissions.Extensions["pubkey-fp"]
			if conn.Permissions.Extensions["comment"] != "" {
				keyId = conn.Permissions.Extensions["comment"]
			}

			fmt.Fprintf(tty, "%s\n%s\n%s\n%s\n", id, keyId, users.NormaliseHostname(conn.User()), conn.RemoteAddr().String())
		}

		return nil
	}

	// 处理--shell-completion参数，生成自动补全脚本
	if line.IsSet("shell-completion") {
		originalServerName, err := line.GetArgString("shell-completion")
		if err != nil {
			originalServerName = "rssh" // 默认服务器名
		}

		serverConsoleAddress := originalServerName

		// 处理带端口的服务器名
		host, port, err := net.SplitHostPort(originalServerName)
		if err == nil {
			serverConsoleAddress = host + " -p " + port
		}

		// 替换模板中的占位符
		res := strings.ReplaceAll(completion, "REPLACEMEWITH_THE_REAL_SERVER_NAME_4259e892-f7ca-4428-afb0-9af135ce9458", serverConsoleAddress)
		res = strings.ReplaceAll(res, "REPLACEMEWITH_JUMPHOST_THE_REAL_SERVER_NAME_6e020f45-6d31-4c98-af4d-0ba75b48b664", originalServerName)

		fmt.Fprintln(tty, res)
		return nil
	}

	return nil
}

// Expect 方法返回自动补全的期望输入类型
func (k *shellAutocomplete) Expect(line terminal.ParsedLine) []string {
	return nil
}

// Help 方法返回shellAutocomplete命令的帮助信息
func (k *shellAutocomplete) Help(explain bool) string {
	if explain {
		return "Generate bash/zsh autocompletion, or match clients and return list of ids" // 简要说明
	}

	// 完整帮助信息
	return terminal.MakeHelpText(
		k.ValidArgs(),  // 有效参数列表
		"autocomplete", // 使用语法
	)
}
