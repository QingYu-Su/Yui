package commands

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/QingYu-Su/Yui/internal"                       // 内部核心模块
	"github.com/QingYu-Su/Yui/internal/server/multiplexer"    // 多路复用器
	"github.com/QingYu-Su/Yui/internal/server/observers"      // 观察者模块
	"github.com/QingYu-Su/Yui/internal/server/users"          // 用户管理
	"github.com/QingYu-Su/Yui/internal/terminal"              // 终端处理
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete" // 自动补全
	"github.com/QingYu-Su/Yui/pkg/logger"                     // 日志记录
	"golang.org/x/crypto/ssh"                                 // SSH协议库
)

// autostartEntry 结构体用于存储自动启动的条目信息
type autostartEntry struct {
	ObserverID string // 观察者ID
	Criteria   string // 匹配条件（用于匹配客户端）
}

// autoStartServerPort 存储了客户端监听端口到自动启动观察者的映射
// 比如127.0.0.1:8080到观察者，一旦新客户端满足观察者条件，则会自动开启端口
var autoStartServerPort = map[internal.RemoteForwardRequest]autostartEntry{}

// listen 结构体定义了监听命令的类型
type listen struct {
	log logger.Logger // 日志记录器
}

// server 方法处理监听服务器的操作
// 参数:
//   - tty: 终端输入输出接口
//   - line: 解析后的命令行参数
//   - onAddrs: 需要启动监听的地址列表
//   - offAddrs: 需要停止监听的地址列表
//
// 返回值: 执行过程中出现的错误
func (l *listen) server(tty io.ReadWriter, line terminal.ParsedLine, onAddrs, offAddrs []string) error {
	// 如果设置了-l参数，列出当前所有活跃的监听器
	if line.IsSet("l") {
		listeners := multiplexer.ServerMultiplexer.GetListeners()

		// 检查是否有活跃的监听器
		if len(listeners) == 0 {
			fmt.Fprintln(tty, "No active listeners")
			return nil
		}

		// 输出所有监听器地址
		for _, listener := range listeners {
			fmt.Fprintf(tty, "%s\n", listener)
		}
		return nil
	}

	// 启动指定的监听地址
	for _, addr := range onAddrs {
		err := multiplexer.ServerMultiplexer.StartListener("tcp", addr)
		if err != nil {
			return err
		}
		fmt.Fprintln(tty, "started listening on: ", addr)
	}

	// 停止指定的监听地址
	for _, addr := range offAddrs {
		err := multiplexer.ServerMultiplexer.StopListener(addr)
		if err != nil {
			return err
		}
		fmt.Fprintln(tty, "stopped listening on: ", addr)
	}

	return nil
}

// client 方法处理客户端监听器的管理
func (l *listen) client(user *users.User, tty io.ReadWriter, line terminal.ParsedLine, onAddrs, offAddrs []string) error {
	// 检查是否启用自动模式和列表模式
	auto := line.IsSet("auto")
	if line.IsSet("l") && auto {
		// 列出所有自动启动的端口转发配置
		for k, v := range autoStartServerPort {
			fmt.Fprintf(tty, "%s %s\n", v.Criteria, net.JoinHostPort(k.BindAddr, fmt.Sprintf("%d", k.BindPort)))
		}
		return nil
	}

	// 获取客户端标识符参数
	specifier, err := line.GetArgString("c")
	if err != nil {
		specifier, err = line.GetArgString("client")
		if err != nil {
			return err
		}
	}

	// 根据标识符查找匹配的客户端
	foundClients, err := user.SearchClients(specifier)
	if err != nil {
		return err
	}

	// 检查是否找到匹配的客户端
	if len(foundClients) == 0 && !auto {
		return fmt.Errorf("No clients matched '%s'", specifier)
	}

	// 如果是列表模式，显示客户端当前的端口转发配置
	if line.IsSet("l") {
		for id, cc := range foundClients {
			// 查询客户端的TCP/IP转发状态
			result, message, _ := cc.SendRequest("query-tcpip-forwards", true, nil)
			if !result {
				fmt.Fprintf(tty, "%s does not support querying server forwards\n", id)
				continue
			}

			// 解析返回的转发信息
			f := struct {
				RemoteForwards []string
			}{}
			err := ssh.Unmarshal(message, &f)
			if err != nil {
				fmt.Fprintf(tty, "%s sent an incompatiable message: %s\n", id, err)
				continue
			}

			// 输出客户端信息和其端口转发配置
			fmt.Fprintf(tty, "%s (%s %s): \n", id, users.NormaliseHostname(cc.User()), cc.RemoteAddr().String())
			for _, rf := range f.RemoteForwards {
				fmt.Fprintf(tty, "\t%s\n", rf)
			}
		}
		return nil
	}

	// 准备要启动的端口转发请求
	var fwRequests []internal.RemoteForwardRequest
	for _, addr := range onAddrs {
		ip, port, err := net.SplitHostPort(addr)
		if err != nil {
			return err
		}

		p, err := strconv.ParseInt(port, 10, 32)
		if err != nil {
			return err
		}

		fwRequests = append(fwRequests, internal.RemoteForwardRequest{
			BindPort: uint32(p),
			BindAddr: ip,
		})
	}

	// 处理每个转发请求
	for _, r := range fwRequests {
		b := ssh.Marshal(&r)
		applied := len(foundClients)

		// 向每个匹配的客户端发送转发请求
		for c, sc := range foundClients {
			result, message, err := sc.SendRequest("tcpip-forward", true, b)
			if !result {
				applied--
				fmt.Fprintln(tty, "failed to start port on (client may not support it): ", c, ": ", string(message))
				continue
			}

			if err != nil {
				applied--
				fmt.Fprintln(tty, "error starting port on: ", c, ": ", err)
			}
		}

		fmt.Fprintf(tty, "started %s on %d clients (total %d)\n",
			net.JoinHostPort(r.BindAddr, fmt.Sprintf("%d", r.BindPort)),
			applied,
			len(foundClients))

		// 如果启用了自动模式，注册观察者以在新客户端连接时自动设置转发
		if auto {
			var entry autostartEntry
			entry.ObserverID = observers.ConnectionState.Register(func(c observers.ClientState) {
				if !user.Matches(specifier, c.ID, c.IP) || c.Status == "disconnected" {
					return
				}

				client, err := user.GetClient(c.ID)
				if err != nil {
					return
				}

				result, message, err := client.SendRequest("tcpip-forward", true, b)
				if !result {
					l.log.Warning("failed to start server tcpip-forward on client: %s: %s", c.ID, message)
					return
				}

				if err != nil {
					l.log.Warning("error auto starting port on: %s: %s", c.ID, err)
					return
				}
			})

			entry.Criteria = specifier
			autoStartServerPort[r] = entry
		}
	}

	// 准备要取消的端口转发请求
	var cancelFwRequests []internal.RemoteForwardRequest
	for _, addr := range offAddrs {
		ip, port, err := net.SplitHostPort(addr)
		if err != nil {
			return err
		}

		p, err := strconv.ParseInt(port, 10, 32)
		if err != nil {
			return err
		}

		cancelFwRequests = append(cancelFwRequests, internal.RemoteForwardRequest{
			BindPort: uint32(p),
			BindAddr: ip,
		})
	}

	// 处理每个取消转发请求
	for _, r := range cancelFwRequests {
		applied := len(foundClients)
		b := ssh.Marshal(&r)

		// 向每个匹配的客户端发送取消转发请求
		for c, sc := range foundClients {
			result, message, err := sc.SendRequest("cancel-tcpip-forward", true, b)
			if !result {
				applied--
				fmt.Fprintln(tty, "failed to stop port on: ", c, ": ", string(message))
				continue
			}

			if err != nil {
				applied--
				fmt.Fprintln(tty, "error stop port on: ", c, ": ", err)
			}
		}

		fmt.Fprintf(tty, "stopped %s on %d clients\n",
			net.JoinHostPort(r.BindAddr, fmt.Sprintf("%d", r.BindPort)),
			applied)

		// 如果启用了自动模式，取消相关的观察者注册
		if auto {
			if _, ok := autoStartServerPort[r]; ok {
				observers.ConnectionState.Deregister(autoStartServerPort[r].Criteria)
			}
			delete(autoStartServerPort, r)
		}
	}

	return nil
}

// ValidArgs 方法返回 listen 命令的有效参数及其描述
func (w *listen) ValidArgs() map[string]string {
	r := map[string]string{
		"on":   "Turn on port, e.g --on :8080 127.0.0.1:4444",                                                                                    // 开启端口
		"auto": "Automatically turn on server control port on clients that match criteria, (use --off --auto to disable and --l --auto to view)", // 自动模式
		"off":  "Turn off port, e.g --off :8080 127.0.0.1:4444",                                                                                  // 关闭端口
		"l":    "List all enabled addresses",                                                                                                     // 列出所有已启用的地址
	}

	// 添加客户端和服务器的重复标志参数
	addDuplicateFlags("Open server port on client/s takes a pattern, e.g -c *, --client your.hostname.here", r, "client", "c")
	addDuplicateFlags("Change the server listeners", r, "server", "s")

	return r
}

// Run 方法是 listen 命令的主执行方法
func (w *listen) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 获取要开启的端口列表
	onAddrs, err := line.GetArgsString("on")
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}
	if len(onAddrs) == 0 && err != terminal.ErrFlagNotSet {
		return errors.New("no value specified for --on, requires port e.g --on :4343")
	}

	// 获取要关闭的端口列表
	offAddrs, err := line.GetArgsString("off")
	if err != nil && err != terminal.ErrFlagNotSet {
		return err
	}
	if len(offAddrs) == 0 && err != terminal.ErrFlagNotSet {
		return errors.New("no value specified for --off, requires port e.g --off :4343")
	}

	// 检查是否有可执行的操作
	if onAddrs == nil && offAddrs == nil && !line.IsSet("l") {
		return errors.New("no actionable argument supplied, please add --on, --off or -l (list)")
	}

	// 根据参数决定是操作服务器还是客户端
	if line.IsSet("server") || line.IsSet("s") {
		return w.server(tty, line, onAddrs, offAddrs)
	} else if line.IsSet("client") || line.IsSet("c") || line.IsSet("auto") {
		return w.client(user, tty, line, onAddrs, offAddrs)
	}

	return errors.New("neither server or client were specified, please choose one")
}

// Expect 方法返回自动补全的期望输入类型
func (W *listen) Expect(line terminal.ParsedLine) []string {
	// 如果当前输入的是客户端部分，提供远程ID的自动补全
	if line.Section != nil {
		switch line.Section.Value() {
		case "c", "client":
			return []string{autocomplete.RemoteId}
		}
	}
	return nil
}

// Help 方法返回 listen 命令的帮助信息
func (w *listen) Help(explain bool) string {
	if explain {
		return "Change, add or stop rssh server port. Open the server port on a client (proxy)" // 简要说明
	}

	// 完整帮助信息
	return terminal.MakeHelpText(
		w.ValidArgs(),            // 有效参数列表
		"listen [OPTION] [PORT]", // 使用语法
		"listen starts or stops listening control ports", // 简短描述
		"it allows you to change the servers listening port, or open the servers control port on an rssh client, so that forwarding is easier", // 详细说明
	)
}

// Listen 函数是 listen 命令的构造函数
func Listen(log logger.Logger) *listen {
	return &listen{
		log: log, // 初始化日志记录器
	}
}
