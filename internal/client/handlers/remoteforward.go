package handlers

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/client/connection"
	"golang.org/x/crypto/ssh"
)

// remoteforward 结构体表示一个远程端口转发配置
type remoteforward struct {
	Listener net.Listener        // 本地监听器
	User     *connection.Session // 关联的用户会话
}

var (
	// 用于保护 currentRemoteForwards 的读写锁
	currentRemoteForwardsLck sync.RWMutex
	// 存储所有活动的远程端口转发配置
	currentRemoteForwards = map[internal.RemoteForwardRequest]remoteforward{}
)

// GetServerRemoteForwards 获取所有服务器远程端口转发配置
// 返回值: out - 包含所有转发配置字符串表示的切片
func GetServerRemoteForwards() (out []string) {
	currentRemoteForwardsLck.RLock()         // 获取读锁
	defer currentRemoteForwardsLck.RUnlock() // 确保锁释放

	// 遍历所有转发配置
	for a, c := range currentRemoteForwards {
		// 只返回没有关联用户会话的转发配置
		if c.User == nil {
			out = append(out, a.String())
		}
	}

	return out
}

// StopAllRemoteForwards 停止所有远程端口转发
func StopAllRemoteForwards() {
	currentRemoteForwardsLck.Lock()         // 获取写锁
	defer currentRemoteForwardsLck.Unlock() // 确保锁释放

	// 异步关闭所有监听器
	for _, forward := range currentRemoteForwards {
		go forward.Listener.Close()
	}

	// 清空转发配置映射
	clear(currentRemoteForwards)
}

// StopRemoteForward 停止指定的远程端口转发
// 参数: rf - 要停止的远程转发请求
// 返回值: error - 如果停止过程中出现错误
func StopRemoteForward(rf internal.RemoteForwardRequest) error {
	currentRemoteForwardsLck.Lock()         // 获取写锁
	defer currentRemoteForwardsLck.Unlock() // 确保锁释放

	// 检查转发配置是否存在
	if _, ok := currentRemoteForwards[rf]; !ok {
		return fmt.Errorf("unable to find remote forward request")
	}

	// 关闭监听器并从映射中删除配置
	currentRemoteForwards[rf].Listener.Close()
	delete(currentRemoteForwards, rf)

	log.Println("Stopped listening on: ", rf.BindAddr, rf.BindPort)

	return nil
}

// StartRemoteForward 启动远程端口转发
// 参数:
//
//	session - 关联的用户会话(可为nil)
//	r - SSH请求对象，包含转发配置
//	sshConn - SSH连接对象
func StartRemoteForward(session *connection.Session, r *ssh.Request, sshConn ssh.Conn) {
	// 1. 解析SSH请求中的转发配置
	var rf internal.RemoteForwardRequest
	err := ssh.Unmarshal(r.Payload, &rf)
	if err != nil {
		r.Reply(false, []byte(fmt.Sprintf("解析远程转发请求失败: %s", err.Error())))
		return
	}

	// 2. 在本地创建TCP监听器
	l, err := net.Listen("tcp", net.JoinHostPort(rf.BindAddr, fmt.Sprintf("%d", rf.BindPort)))
	if err != nil {
		r.Reply(false, []byte(fmt.Sprintf("创建监听器失败: %s", err.Error())))
		return
	}
	defer l.Close() // 确保函数退出时关闭监听器

	// 3. 确保在函数退出时停止转发
	defer StopRemoteForward(rf)

	// 4. 如果存在关联会话，记录支持的转发类型
	if session != nil {
		session.Lock()
		session.SupportedRemoteForwards[rf] = true
		session.Unlock()
	}

	// 5. 处理动态端口分配(RFC 4254)
	responseData := []byte{}
	if rf.BindPort == 0 { // 如果请求端口为0，表示需要动态分配
		port := uint32(l.Addr().(*net.TCPAddr).Port) // 获取实际分配的端口
		responseData = ssh.Marshal(port)
		rf.BindPort = port // 更新转发配置中的端口号
	}
	r.Reply(true, responseData) // 响应SSH请求

	log.Println("开始在本地监听: ", l.Addr())

	// 6. 记录当前转发配置
	currentRemoteForwardsLck.Lock()
	currentRemoteForwards[rf] = remoteforward{
		Listener: l,
		User:     session,
	}
	currentRemoteForwardsLck.Unlock()

	// 7. 主循环：接受传入连接
	for {
		proxyCon, err := l.Accept()
		if err != nil {
			return // 监听器关闭时退出
		}
		// 为每个连接启动goroutine处理
		go handleData(rf, proxyCon, sshConn)
	}
}

// handleData 处理单个转发连接的数据传输
// 参数:
//
//	rf - 远程转发配置
//	proxyCon - 本地TCP连接
//	sshConn - SSH连接对象
//
// 返回值:
//
//	error - 处理过程中发生的错误
func handleData(rf internal.RemoteForwardRequest, proxyCon net.Conn, sshConn ssh.Conn) error {
	log.Println("接受新连接: ", proxyCon.RemoteAddr())

	// 1. 解析连接来源信息
	originatorAddress, originatorPort, err := net.SplitHostPort(proxyCon.LocalAddr().String())
	if err != nil {
		return err
	}
	originatorPortInt, err := strconv.ParseInt(originatorPort, 10, 32)
	if err != nil {
		return err
	}

	// 2. 构造SSH直接通道消息(RFC 4254 7.2)
	drtMsg := internal.ChannelOpenDirectMsg{
		Raddr: originatorAddress,         // 原始连接来源地址
		Rport: uint32(originatorPortInt), // 原始连接来源端口
		Laddr: rf.BindAddr,               // 转发目标地址
		Lport: rf.BindPort,               // 转发目标端口
	}
	log.Printf("构造的直接通道消息: %+v", drtMsg)

	// 3. 在SSH连接上打开转发通道
	b := ssh.Marshal(&drtMsg)
	source, reqs, err := sshConn.OpenChannel("forwarded-tcpip", b)
	if err != nil {
		log.Println("打开forwarded-tcpip通道失败: ", err)
		return err
	}
	defer source.Close()

	// 4. 丢弃不需要的通道请求
	go ssh.DiscardRequests(reqs)

	log.Println("forwarded-tcpip通道请求已发送并被接受")

	// 5. 启动双向数据转发
	go func() {
		defer source.Close()
		defer proxyCon.Close()
		io.Copy(source, proxyCon) // 本地→远程
	}()

	defer proxyCon.Close()
	_, err = io.Copy(proxyCon, source) // 远程→本地

	return err
}
