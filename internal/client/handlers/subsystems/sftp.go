package subsystems

import (
	"fmt"
	"io"

	"github.com/QingYu-Su/Yui/internal/terminal"
	"github.com/pkg/sftp"     // SFTP协议实现库
	"golang.org/x/crypto/ssh" // SSH协议库
)

// subSftp 类型定义SFTP子系统标识
// 实现bool类型用于开关控制，实际作为子系统标识符使用
type subSftp bool

// Execute 方法实现SFTP子系统的核心逻辑
// 参数说明：
//   - _ : 忽略命令行参数（SFTP协议通过独立通道通信）
//   - connection: 已建立的SSH通道连接
//   - subsystemReq: SFTP子系统请求对象
//
// 返回值：
//   - error: 返回服务运行期间的错误（io.EOF表示客户端正常断开）
func (s *subSftp) Execute(_ terminal.ParsedLine, connection ssh.Channel, subsystemReq *ssh.Request) error {
	// 创建SFTP服务器实例
	// 注意：connection会被sftp服务器接管，无需手动关闭
	server, err := sftp.NewServer(connection)
	if err != nil {
		// 初始化失败时向客户端返回错误详情
		subsystemReq.Reply(false, []byte(err.Error()))
		return err
	}

	// 确认子系统启动成功
	subsystemReq.Reply(true, nil)

	// 启动SFTP服务（阻塞运行直到连接终止）
	// 注意：server.Serve()会自动处理协议协商和请求分发
	err = server.Serve()

	// 错误处理：
	// - io.EOF 表示客户端正常断开连接
	// - 其他错误表示异常终止
	if err != io.EOF && err != nil {
		return fmt.Errorf("sftp server had an error: %s", err.Error())
	}

	// 正常退出（无错误）
	return nil
}
