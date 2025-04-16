package commands

import (
	"fmt"
	"io"
	"sync"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/internal/terminal"
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// connect 结构体实现客户端连接功能
type connect struct {
	log     logger.Logger // 日志记录器
	user    *users.User   // 当前用户
	session string        // 会话ID
}

// ValidArgs 定义命令支持的参数
func (c *connect) ValidArgs() map[string]string {
	return map[string]string{
		"shell": "Set the shell (or program) to start on connection, this also takes an http, https or rssh url that be downloaded to disk and executed",
	}
}

// Run 方法是connect命令的主要执行逻辑
func (c *connect) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 获取当前会话
	sess, err := c.user.Session(c.session)
	if err != nil {
		return err
	}

	// 检查是否分配了PTY（伪终端）
	if sess.Pty == nil {
		return fmt.Errorf("Connect requires a pty")
	}

	// 检查是否为真实终端
	term, ok := tty.(*terminal.Terminal)
	if !ok {
		return fmt.Errorf("connect can only be called from the terminal, if you want to connect to your clients without connecting to the terminal use jumphost syntax -J")
	}

	// 检查是否提供了客户端参数
	if len(line.Arguments) < 1 {
		return fmt.Errorf("%s", c.Help(false)) // 显示帮助信息
	}

	// 获取shell参数（可选）
	shell, _ := line.GetArgString("shell")

	// 获取目标客户端标识（最后一个参数）
	client := line.Arguments[len(line.Arguments)-1].Value()

	// 搜索匹配的客户端
	foundClients, err := user.SearchClients(client)
	if err != nil {
		return err
	}

	// 检查是否找到匹配的客户端
	if len(foundClients) == 0 {
		return fmt.Errorf("No clients matched '%s'", client)
	}

	// 检查是否匹配到多个客户端
	if len(foundClients) > 1 {
		return fmt.Errorf("'%s' matches multiple clients please choose a more specific identifier", client)
	}

	// 获取第一个匹配的客户端连接（Go map遍历的惯用方式）
	var target ssh.Conn
	for k := range foundClients {
		target = foundClients[k]
		break
	}

	// 确保连接最终会被关闭
	defer func() {
		c.log.Info("Disconnected from remote host %s (%s)", target.RemoteAddr(), target.ClientVersion())
		term.DisableRaw() // 禁用终端原始模式
	}()

	// 创建新的SSH会话
	newSession, err := createSession(target, *sess.Pty, shell)
	if err != nil {
		c.log.Error("Creating session failed: %s", err)
		return err
	}

	c.log.Info("Connected to %s", target.RemoteAddr().String())

	// 启用终端原始模式并附加会话
	term.EnableRaw()
	err = attachSession(newSession, term, sess.ShellRequests)
	if err != nil {
		c.log.Error("Client tried to attach session and failed: %s", err)
		return err
	}

	// 返回会话终止信息（虽然使用error返回，但实际上是正常结束）
	return fmt.Errorf("Session has terminated.")
}

// Expect 方法实现命令的自动补全逻辑
func (c *connect) Expect(line terminal.ParsedLine) []string {
	// 当参数数量小于等于1时（即命令名后没有或只有1个参数时）
	if len(line.Arguments) <= 1 {
		// 提供远程ID的自动补全建议
		return []string{autocomplete.RemoteId}
	}
	// 其他情况不提供自动补全
	return nil
}

// Help 方法提供命令的帮助信息
func (c *connect) Help(explain bool) string {
	// 命令功能描述
	const description = "Start shell on remote controllable host."

	// 简要说明模式（当explain为true时）
	if explain {
		return description
	}

	// 完整帮助信息模式
	return terminal.MakeHelpText(
		c.ValidArgs(),                    // 获取参数说明
		"connect "+autocomplete.RemoteId, // 命令使用示例（展示需要远程ID参数）
		description,                      // 功能描述
	)
}

// Connect 是connect命令的工厂函数，用于创建connect实例
func Connect(
	session string,
	user *users.User,
	log logger.Logger) *connect {
	return &connect{
		session: session, // 设置会话ID
		user:    user,    // 设置用户对象
		log:     log,     // 设置日志记录器
	}
}

// createSession 创建SSH会话并设置PTY和shell
func createSession(sshConn ssh.Conn, ptyReq internal.PtyReq, shell string) (sc ssh.Channel, err error) {
	// 打开SSH会话通道
	splice, newrequests, err := sshConn.OpenChannel("session", nil)
	if err != nil {
		return sc, fmt.Errorf("Unable to start remote session on host %s (%s) : %s",
			sshConn.RemoteAddr(), sshConn.ClientVersion(), err)
	}

	// 发送PTY请求（伪终端设置），跟随当前用户终端设置
	_, err = splice.SendRequest("pty-req", true, ssh.Marshal(ptyReq))
	if err != nil {
		return sc, fmt.Errorf("Unable to send PTY request: %s", err)
	}

	// 发送shell启动请求（可指定自定义shell命令）
	_, err = splice.SendRequest("shell", true, ssh.Marshal(internal.ShellStruct{Cmd: shell}))
	if err != nil {
		return sc, fmt.Errorf("Unable to start shell: %s", err)
	}

	// newrequests是用于对方请求的通道，可以直接丢弃
	go ssh.DiscardRequests(newrequests)

	return splice, nil
}

// attachSession 将会话附加到当前终端，处理双向IO和请求转发
func attachSession(
	newSession ssh.Channel,
	currentClientSession io.ReadWriter,
	currentClientRequests <-chan *ssh.Request) error {
	// 创建完成信号通道
	finished := make(chan bool)

	// 定义关闭函数，用于清理资源
	close := func() {
		newSession.Close() // 关闭远程会话
		close(finished)    // 关闭完成通道，停止请求转发
	}

	// 确保最终会执行关闭操作
	var once sync.Once
	defer once.Do(close)

	// 启动goroutine处理用户输入（本地->远程）
	go func() {
		io.Copy(newSession, currentClientSession) // 将本地输入转发到远程
		once.Do(close)                            // 完成后关闭
	}()

	// 启动goroutine处理远程输出（远程->本地）
	go func() {
		io.Copy(currentClientSession, newSession) // 将远程输出转发到本地
		once.Do(close)                            // 完成后关闭
	}()

	// 请求代理循环，转发客户端请求到远程会话
RequestsProxyPasser:
	for {
		select {
		case r := <-currentClientRequests: // 收到客户端请求
			// 转发请求到远程会话
			response, err := internal.SendRequest(*r, newSession)
			if err != nil {
				break RequestsProxyPasser // 出错时退出循环
			}

			// 如果需要回复，则回复客户端
			if r.WantReply {
				r.Reply(response, nil)
			}
		case <-finished: // 收到完成信号
			break RequestsProxyPasser // 退出循环
		}
	}

	return nil
}
