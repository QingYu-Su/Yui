//go:build linux

package subsystems

import (
	"fmt"
	"strconv"
	"syscall"

	"github.com/QingYu-Su/Yui/internal/terminal"
	"golang.org/x/crypto/ssh"
)

// setgid 子系统实现Linux系统的GID设置功能
type setgid bool

// Execute 实现setgid子系统的命令处理逻辑
// 参数:
//   - line: 解析后的命令行参数
//   - connection: SSH通道连接
//   - subsystemReq: 子系统请求对象
//
// 返回值:
//   - error: 执行过程中产生的错误
func (su *setgid) Execute(line terminal.ParsedLine, connection ssh.Channel, subsystemReq *ssh.Request) error {
	// 确认子系统请求成功
	subsystemReq.Reply(true, nil)

	// 参数校验：必须且只能有1个参数
	if len(line.Arguments) != 1 {
		fmt.Fprintf(connection, "使用方法: setgid <gid>\n只需指定一个GID参数")
		return nil
	}

	// 转换GID参数为整型
	gid, err := strconv.Atoi(line.Arguments[0].Value())
	if err != nil {
		fmt.Fprintf(connection, "无效的GID格式: %s", err.Error())
		return nil
	}

	// 执行系统调用设置GID
	err = syscall.Setgid(gid)
	if err != nil {
		fmt.Fprintf(connection, "设置GID失败: %v", err)
		return nil
	}

	fmt.Fprintf(connection, "成功设置GID为%d", gid)
	return nil
}
