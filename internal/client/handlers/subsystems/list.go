package subsystems

import (
	"fmt"

	"github.com/QingYu-Su/Yui/internal/terminal"
	"golang.org/x/crypto/ssh"
)

// list 子系统实现，用于列出所有可用的子系统
type list bool

// Execute 执行list子系统的命令逻辑
// 参数:
//   - line: 解析后的命令行输入
//   - connection: SSH通道连接
//   - subsystemReq: 子系统请求对象
//
// 返回值:
//   - error: 执行过程中产生的错误
func (l *list) Execute(line terminal.ParsedLine, connection ssh.Channel, subsystemReq *ssh.Request) error {
	// 首先确认子系统请求成功
	subsystemReq.Reply(true, nil)

	// 遍历所有注册的子系统并输出名称
	for k := range subsystems {
		fmt.Fprintf(connection, "%s\n", k)
	}

	return nil
}
