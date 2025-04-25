//go:build linux

package subsystems

import (
	"fmt"
	"strconv"
	"syscall"

	"github.com/QingYu-Su/Yui/internal/terminal"
	"golang.org/x/crypto/ssh"
)

// setuid 子系统实现Linux系统的UID设置功能
type setuid bool

// Execute 实现setuid子系统的命令处理逻辑
// 参数:
//   - line: 解析后的命令行参数（需包含目标UID）
//   - connection: SSH通道连接（用于返回结果）
//   - subsystemReq: 子系统请求对象
//
// 返回值:
//   - error: 执行过程中产生的错误（已通过connection返回给客户端）
func (su *setuid) Execute(line terminal.ParsedLine, connection ssh.Channel, subsystemReq *ssh.Request) error {
	// 确认子系统请求成功
	subsystemReq.Reply(true, nil)

	// 参数校验：必须且只能有1个参数
	if len(line.Arguments) != 1 {
		fmt.Fprintf(connection, "错误：参数数量不正确\n用法: setuid <UID>\n示例: setuid 1000")
		return nil
	}

	// 转换UID参数为整型
	uid, err := strconv.Atoi(line.Arguments[0].Value())
	if err != nil {
		fmt.Fprintf(connection, "错误：无效的UID格式\n原因: %v\nUID必须是数字", err)
		return nil
	}

	// UID范围安全检查 (常规Linux系统UID范围)
	if uid < 0 || uid > 60000 {
		fmt.Fprintf(connection, "错误：UID值越界\n有效范围: 0-60000\n当前值: %d", uid)
		return nil
	}

	// 获取当前进程权限信息
	currentEuid := syscall.Geteuid()
	if currentEuid != 0 && uid != currentEuid {
		fmt.Fprintf(connection, "错误：权限不足\n只有root用户或当前用户(%d)可执行此操作", currentEuid)
		return nil
	}

	// 执行系统调用设置UID
	if err := syscall.Setuid(uid); err != nil {
		fmt.Fprintf(connection, "错误：设置UID失败\n原因: %v\n建议: 检查目标UID是否存在", err)
		return nil
	}

	// 验证设置结果
	if syscall.Getuid() != uid || syscall.Geteuid() != uid {
		fmt.Fprintf(connection, "警告：UID设置未完全生效\n当前UID: %d (实际: %d)\n当前EUID: %d (实际: %d)",
			uid, syscall.Getuid(), uid, syscall.Geteuid())
		return nil
	}

	fmt.Fprintf(connection, "成功设置UID为%d\n当前身份: uid=%d euid=%d", uid, syscall.Getuid(), syscall.Geteuid())
	return nil
}
