package subsystems

import (
	"fmt"

	"github.com/QingYu-Su/Yui/internal/terminal"
	"golang.org/x/crypto/ssh"
)

// 全局子系统注册表
// key: 子系统名称 (如"sftp", "list")
// value: 子系统实现实例
// 注意: 这里同时支持Windows和Linux平台的SFTP
var subsystems = map[string]subsystem{
	"sftp": new(subSftp), // SFTP文件传输子系统
	"list": new(list),    // 子系统列表查询功能
}

// subsystem 接口定义
// 所有子系统必须实现Execute方法
type subsystem interface {
	// Execute 执行子系统核心逻辑
	// arguments: 解析后的命令行参数
	// connection: SSH通道连接
	// subsystemReq: 子系统请求对象
	Execute(arguments terminal.ParsedLine, connection ssh.Channel, subsystemReq *ssh.Request) error
}

// RunSubsystems 运行请求的子系统
// connection: 已建立的SSH通道连接
// req: 包含子系统请求信息的SSH请求
// 返回值: 执行过程中发生的错误
func RunSubsystems(connection ssh.Channel, req *ssh.Request) error {
	// 检查Payload长度是否合法
	// SSH协议要求Payload前4字节为字符串长度
	if len(req.Payload) < 4 {
		return fmt.Errorf("Payload size is too small <4, not enough space for token")
	}

	// 解析Payload获取子系统命令
	// 跳过前4字节的长度标识，解析剩余部分
	line := terminal.ParseLine(string(req.Payload[4:]), 0)

	// 查找并执行对应的子系统
	if subsys, ok := subsystems[line.Command.Value()]; ok {
		return subsys.Execute(line, connection, req)
	}

	// 未找到匹配的子系统时返回错误
	req.Reply(false, []byte("Unknown subsystem"))
	return fmt.Errorf("Unknown subsystem '%s'", req.Payload)
}
