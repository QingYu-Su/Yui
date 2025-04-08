package mux

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QingYu-Su/Yui/pkg/mux/protocols"
	"golang.org/x/net/websocket"
)

// MultiplexerConfig 是一个结构体，用于配置多路复用器（Multiplexer）的行为。
type MultiplexerConfig struct {
	Control   bool // 是否启用控制功能
	Downloads bool // 是否启用下载功能

	TLS               bool   // 是否启用 TLS 加密
	AutoTLSCommonName string // 自动 TLS 证书的通用名称（Common Name）

	TLSCertPath string // TLS 证书文件路径
	TLSKeyPath  string // TLS 私钥文件路径

	TcpKeepAlive int // TCP 保活时间间隔（秒）

	PollingAuthChecker func(key string, addr net.Addr) bool // 轮询认证检查器，用于验证客户端身份

	tlsConfig *tls.Config // 内部使用的 TLS 配置
}

// genX509KeyPair 生成一个自签名的 X.509 证书和私钥对。
// 参数 AutoTLSCommonName 是证书的通用名称（Common Name）。
// 返回值是一个 tls.Certificate 对象，包含证书和私钥，以及可能发生的错误。
func genX509KeyPair(AutoTLSCommonName string) (tls.Certificate, error) {
	// 获取当前时间
	now := time.Now()

	// 创建 X.509 证书模板
	template := &x509.Certificate{
		// 证书序列号，使用当前时间的 Unix 时间戳
		SerialNumber: big.NewInt(now.Unix()),
		// 证书主题信息
		Subject: pkix.Name{
			// 通用名称（Common Name），由参数 AutoTLSCommonName 指定
			CommonName: AutoTLSCommonName,
			// 国家
			Country: []string{"US"},
			// 组织
			Organization: []string{"Cloudflare, Inc"},
		},
		// 证书有效期起始时间
		NotBefore: now,
		// 证书有效期结束时间，从当前时间起 30 天后
		NotAfter: now.AddDate(0, 0, 30),
		// 主题密钥标识符
		SubjectKeyId: []byte{113, 117, 105, 99, 107, 115, 101, 114, 118, 101},
		// 基本约束有效
		BasicConstraintsValid: true,
		// 是否是证书颁发机构（CA）
		IsCA: true,
		// 扩展密钥用途，包含服务器认证
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		// 密钥用途，包含密钥加密、数字签名和证书签名
		KeyUsage: x509.KeyUsageKeyEncipherment |
			x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}

	// 生成一个 2048 位的 RSA 私钥
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		// 如果生成私钥失败，返回错误
		return tls.Certificate{}, err
	}

	// 使用模板和私钥生成 X.509 证书
	cert, err := x509.CreateCertificate(rand.Reader, template, template,
		priv.Public(), priv)
	if err != nil {
		// 如果生成证书失败，返回错误
		return tls.Certificate{}, err
	}

	// 创建一个 tls.Certificate 对象，包含生成的证书和私钥
	var outCert tls.Certificate
	outCert.Certificate = append(outCert.Certificate, cert)
	outCert.PrivateKey = priv

	// 返回生成的证书和私钥
	return outCert, nil
}

// Multiplexer 是一个多路复用器，用于管理多个网络连接和协议。
// 它可以同时监听多个地址，并根据协议类型将连接分发到相应的处理程序。
type Multiplexer struct {
	sync.RWMutex                                           // 用于保护共享资源的读写锁
	result         map[protocols.Type]*multiplexerListener // 存储协议类型与监听器的映射关系
	done           bool                                    // 标记多路复用器是否已经停止
	listeners      map[string]net.Listener                 // 存储监听地址与监听器的映射关系
	newConnections chan net.Conn                           // 用于接收新连接的通道

	config MultiplexerConfig // 多路复用器的配置
}

// StartListener 启动一个网络监听器，监听指定的地址和网络类型。
// 参数：
// - network: 网络类型，如 "tcp" 或 "udp"。
// - address: 监听的地址，如 "127.0.0.1:8080"。
// 返回值：
// - error: 如果启动监听器失败，返回错误；否则返回 nil。
func (m *Multiplexer) StartListener(network, address string) error {
	// 加锁，确保监听器的启动过程是线程安全的
	m.Lock()
	defer m.Unlock()

	// 检查是否已经存在相同的监听地址
	if _, ok := m.listeners[address]; ok {
		// 如果已经存在，返回错误
		return errors.New("Address " + address + " already listening")
	}

	// 根据配置中的 TcpKeepAlive 设置 TCP 保活时间
	d := time.Duration(time.Duration(m.config.TcpKeepAlive) * time.Second)
	if m.config.TcpKeepAlive == 0 {
		// 如果 TcpKeepAlive 为 0，设置为无效值（-1），禁用 keep-alive，连接不主动探测
		d = time.Duration(-1)
	}

	// 创建一个 net.ListenConfig 对象，用于配置监听器的行为
	lc := net.ListenConfig{
		KeepAlive: d,
	}

	// 使用 net.ListenConfig 启动监听器
	listener, err := lc.Listen(context.Background(), network, address)
	if err != nil {
		// 如果启动监听器失败，返回错误
		return err
	}

	// 将监听器存储到 listeners 映射中
	m.listeners[address] = listener

	// 启动一个协程，用于接受新连接
	go func(listen net.Listener) {
		for {
			// 接受新连接
			conn, err := listen.Accept()
			if err != nil {
				// 如果发生错误，检查是否是因为监听器被关闭
				if strings.Contains(err.Error(), "use of closed network connection") {
					// 如果是监听器被关闭，从 listeners 中删除该地址并退出协程
					m.Lock()
					delete(m.listeners, address)
					m.Unlock()
					return
				}
				// 如果是其他错误，忽略并继续尝试接受新连接
				continue
			}

			// 启动一个协程，将新连接发送到 newConnections 通道
			go func() {
				select {
				case m.newConnections <- conn:
					// 如果成功发送到通道，继续处理
				case <-time.After(2 * time.Second):
					// 如果发送超时（2秒内未发送成功），记录日志并关闭连接
					log.Println("Accepting new connection timed out")
					conn.Close()
				}
			}()
		}
	}(listener)

	// 启动监听器成功，返回 nil
	return nil
}

// ConnContextKey 是一个类型别名，用于定义上下文键的类型。
type ConnContextKey string

// contextKey 是一个全局变量，用于在 HTTP 请求的上下文中存储连接对象。
var contextKey ConnContextKey = "conn"

// startHttpServer 启动一个 HTTP 服务器，用于处理 HTTP 请求。
func (m *Multiplexer) startHttpServer() {
	// 获取 HTTP 协议的监听器
	listener := m.getProtoListener(protocols.HTTP)

	// 启动一个协程来运行 HTTP 服务器
	go func(l net.Listener) {
		// 创建一个 HTTP 服务器实例
		srv := &http.Server{
			// 设置读取超时时间为 60 秒
			ReadTimeout: 60 * time.Second,
			// 设置写入超时时间为 60 秒
			WriteTimeout: 60 * time.Second,
			// 设置请求处理器
			Handler: m.collector(listener.Addr()),
			// 设置连接上下文，将连接对象存储到上下文中
			ConnContext: func(ctx context.Context, c net.Conn) context.Context {
				return context.WithValue(ctx, contextKey, c)
			},
		}

		// 启动 HTTP 服务器并监听指定的地址
		log.Println(srv.Serve(l))
	}(listener)
}

// collector 是一个 HTTP 请求处理器，用于处理 HTTP 请求。
func (m *Multiplexer) collector(localAddr net.Addr) http.HandlerFunc {
	// 定义一个局部变量，用于存储每个客户端的连接信息
	var (
		// connections 是一个映射，存储客户端的会话 ID 和对应的连接对象
		connections = map[string]*fragmentedConnection{}
		// lck 是一个互斥锁，用于保护 connections 的线程安全
		lck sync.Mutex
	)

	// 返回一个 HTTP 请求处理函数
	return func(w http.ResponseWriter, req *http.Request) {
		// 如果请求方法不是 HEAD、GET 或 POST，则返回 400 Bad Request
		if req.Method != http.MethodHead && req.Method != http.MethodGet && req.Method != http.MethodPost {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// 加锁，保护 connections 的访问
		lck.Lock()

		// 延迟关闭请求体
		defer req.Body.Close()

		// 从请求 URL 中获取客户端的会话 ID
		id := req.URL.Query().Get("id")
		// 从 connections 中查找对应的连接对象
		c, ok := connections[id]
		if !ok {
			// 如果没有找到对应的连接对象
			defer lck.Unlock()

			// 如果请求方法是 HEAD，则尝试建立一个新的连接
			if req.Method == http.MethodHead {
				// 检查服务器是否已经连接了过多的客户端
				if len(connections) > 2000 {
					log.Println("server has too many polling connections (", len(connections), " limit is 2k")
					http.Error(w, "Server Error", http.StatusInternalServerError)
					return
				}

				// 从请求 URL 中获取客户端的密钥
				key := req.URL.Query().Get("key")

				// 从请求上下文中获取原始连接对象
				var err error
				realConn, ok := req.Context().Value(contextKey).(net.Conn)
				if !ok {
					log.Println("couldnt get real connection address")
					http.Error(w, "Server Error", http.StatusInternalServerError)
					return
				}

				// 调用配置中的认证检查器函数，验证客户端的密钥
				if !m.config.PollingAuthChecker(key, realConn.RemoteAddr()) {
					log.Println("client connected but the key for starting a new polling session was wrong")
					http.Error(w, "Bad Request", http.StatusBadRequest)
					return
				}

				// 创建一个新的连接对象
				c, id, err = NewFragmentCollector(localAddr, realConn.RemoteAddr(), func() {
					// 当连接关闭时，从 connections 中删除对应的会话 ID
					delete(connections, id)
				})
				if err != nil {
					log.Println("error generating new fragment collector: ", err)
					http.Error(w, "Server Error", http.StatusInternalServerError)
					return
				}

				// 将新的连接对象存储到 connections 中
				connections[id] = c

				// 设置一个 HTTP Cookie，存储客户端的会话 ID
				http.SetCookie(w, &http.Cookie{
					Name:  "NID",
					Value: id,
				})

				// 将新的连接对象发送到 C2 协议的连接通道中
				l := m.result[protocols.C2]
				select {
				case l.connections <- c:
				case <-time.After(2 * time.Second):
					// 如果发送失败（超时），记录日志并关闭连接
					log.Println(l.protocol, "Failed to accept new http connection within 2 seconds, closing connection (may indicate high resource usage)")
					c.Close()
					delete(connections, id)
					http.Error(w, "Server Error", http.StatusInternalServerError)
					return
				}

				// 重定向客户端到通知页面
				http.Redirect(w, req, "/notification", http.StatusTemporaryRedirect)
				return
			}

			// 如果客户端没有提供有效的会话 ID，则返回 400 Bad Request
			log.Println("client connected but did not have a valid session id")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// 解锁，允许其他协程访问 connections
		lck.Unlock()

		// 重置连接对象的最后活动时间
		c.IsAlive()

		// 根据请求方法处理请求
		switch req.Method {
		// 如果是 GET 请求，则从连接对象的写缓冲区中读取数据并返回给客户端
		case http.MethodGet:
			_, err := io.Copy(w, c.writeBuffer)
			if err != nil {
				if err == io.EOF {
					return
				}
				c.Close()
			}

		// 如果是 POST 请求，则将客户端发送的数据写入连接对象的读缓冲区
		case http.MethodPost:
			_, err := io.Copy(c.readBuffer, req.Body)
			if err != nil {
				if err == io.EOF {
					return
				}
				c.Close()
			}
		}
	}
}

// StopListener 停止监听指定地址的网络连接。
// 参数：
// - address: 要停止监听的地址。
// 返回值：
// - error: 如果停止监听失败，返回错误；否则返回 nil。
func (m *Multiplexer) StopListener(address string) error {
	// 加锁，确保操作的线程安全
	m.Lock()
	defer m.Unlock()

	// 从 listeners 映射中查找指定地址的监听器
	listener, ok := m.listeners[address]
	if !ok {
		// 如果未找到监听器，返回错误
		return errors.New("Address " + address + " not listening")
	}

	// 调用监听器的 Close 方法停止监听
	return listener.Close()
}

// GetListeners 返回当前所有正在监听的地址列表。
// 返回值：
// - []string: 一个包含所有监听地址的字符串切片。
func (m *Multiplexer) GetListeners() []string {
	// 加读锁，确保操作的线程安全
	m.RLock()
	defer m.RUnlock()

	// 创建一个空切片，用于存储监听地址
	listeners := []string{}
	// 遍历 listeners 映射，将所有监听地址添加到切片中
	for l := range m.listeners {
		listeners = append(listeners, l)
	}

	// 对监听地址切片进行排序，确保返回的列表是有序的
	sort.Strings(listeners)

	// 返回监听地址列表
	return listeners
}

// QueueConn 将一个新连接加入到多路复用器的处理队列中。
// 参数：
// - c: 要加入队列的网络连接。
// 返回值：
// - error: 如果无法将连接加入队列，返回错误；否则返回 nil。
func (m *Multiplexer) QueueConn(c net.Conn) error {
	// 尝试将连接发送到 newConnections 通道
	select {
	case m.newConnections <- c:
		// 如果成功发送，返回 nil
		return nil
	case <-time.After(250 * time.Millisecond):
		// 如果在 250 毫秒内未发送成功，返回错误
		return errors.New("too busy to queue connection")
	}
}

// ListenWithConfig 使用指定的配置启动一个多路复用器，并监听指定的地址。
// 参数：
// - network: 网络类型，如 "tcp" 或 "udp"。
// - address: 监听的地址，如 "127.0.0.1:8080"。
// - _c: 多路复用器的配置。
// 返回值：
// - *Multiplexer: 启动的多路复用器实例。
// - error: 如果启动失败，返回错误；否则返回 nil。
func ListenWithConfig(network, address string, _c MultiplexerConfig) (*Multiplexer, error) {
	// 创建一个多路复用器实例
	var m Multiplexer

	// 初始化多路复用器的通道和映射
	m.newConnections = make(chan net.Conn)               // 用于接收新连接的通道
	m.listeners = make(map[string]net.Listener)          // 用于存储监听器的映射
	m.result = map[protocols.Type]*multiplexerListener{} // 用于存储协议类型与监听器的映射
	m.config = _c                                        // 设置多路复用器的配置

	// 检查是否提供了轮询认证检查器
	if _c.PollingAuthChecker == nil {
		// 如果未提供，返回错误
		return nil, errors.New("no authentication method supplied for polling muxing, this may lead to extreme dos if not set. Must set it")
	}

	// 启动监听器，监听指定的地址和网络类型
	err := m.StartListener(network, address)
	if err != nil {
		// 如果启动监听器失败，返回错误
		return nil, err
	}

	// 根据配置启用控制功能和下载功能
	if m.config.Control {
		// 启用 C2 协议的监听器
		m.result[protocols.C2] = newMultiplexerListener(m.listeners[address].Addr(), protocols.C2)
	}

	if m.config.Downloads {
		// 启用 HTTP 下载协议的监听器
		m.result[protocols.HTTPDownload] = newMultiplexerListener(m.listeners[address].Addr(), protocols.HTTPDownload)
		// 启用 TCP 下载协议的监听器
		m.result[protocols.TCPDownload] = newMultiplexerListener(m.listeners[address].Addr(), protocols.TCPDownload)
	}

	// 启用 HTTP 协议的监听器
	m.result[protocols.HTTP] = newMultiplexerListener(m.listeners[address].Addr(), protocols.HTTP)

	// 启动 HTTP 服务器，用于处理 HTTP 请求
	m.startHttpServer()

	// 定义一个变量，用于记录等待处理的新连接数量
	var waitingConnections int32
	// 启动一个协程，用于处理新连接
	go func() {
		for conn := range m.newConnections {
			// 如果等待处理的新连接数量超过 1000，则关闭新连接并继续
			if atomic.LoadInt32(&waitingConnections) > 1000 {
				conn.Close()
				continue
			}

			// 原子操作，增加等待处理的新连接数量
			atomic.AddInt32(&waitingConnections, 1)
			// 启动一个协程，处理当前连接
			go func(conn net.Conn) {
				// 延迟执行，原子操作，减少等待处理的新连接数量
				defer atomic.AddInt32(&waitingConnections, -1)

				// 解封装连接，获取协议类型和新的连接对象
				newConnection, proto, err := m.unwrapTransports(conn)
				if err != nil {
					// 如果解封装失败，记录日志并返回
					log.Println("Multiplexing failed (unwrapping): ", err)
					return
				}

				// 根据协议类型查找对应的监听器
				l, ok := m.result[proto]
				if !ok {
					// 如果未找到对应的监听器，关闭连接并记录日志
					newConnection.Close()
					log.Println("Multiplexing failed (final determination): ", proto)
					return
				}

				// 将新的连接对象发送到监听器的连接通道中
				select {
				case l.connections <- newConnection:
					// 如果发送成功，继续处理
				case <-time.After(2 * time.Second):
					// 如果发送失败（超时），记录日志并关闭连接
					log.Println(l.protocol, "Failed to accept new connection within 2 seconds, closing connection (may indicate high resource usage)")
					newConnection.Close()
				}
			}(conn)
		}
	}()

	// 返回启动的多路复用器实例
	return &m, nil
}

// Listen 使用默认配置启动一个多路复用器，并监听指定的地址。
// 参数：
// - network: 网络类型，如 "tcp" 或 "udp"。
// - address: 监听的地址，如 "127.0.0.1:8080"。
// 返回值：
// - *Multiplexer: 启动的多路复用器实例。
// - error: 如果启动失败，返回错误；否则返回 nil。
func Listen(network, address string) (*Multiplexer, error) {
	// 创建一个默认的多路复用器配置
	c := MultiplexerConfig{
		Control:      true, // 启用控制功能
		Downloads:    true, // 启用下载功能
		TcpKeepAlive: 7200, // TCP 保活时间为 7200 秒（2 小时），这是 Linux 的默认超时时间
	}

	// 调用 ListenWithConfig 函数，使用默认配置启动多路复用器
	return ListenWithConfig(network, address, c)
}

// Close 关闭多路复用器，停止所有监听器并清理资源。
func (m *Multiplexer) Close() {
	// 设置 done 标志为 true，表示多路复用器即将关闭
	m.done = true

	// 遍历所有监听器，停止监听
	for address := range m.listeners {
		m.StopListener(address)
	}

	// 关闭所有协议的监听器
	for _, v := range m.result {
		v.Close()
	}

	// 关闭新连接通道
	close(m.newConnections)
}

// isHttp 检查给定的字节数据是否符合 HTTP 请求的格式。
// 参数：
// - b: 要检查的字节数据。
// 返回值：
// - bool: 如果数据符合 HTTP 请求格式，返回 true；否则返回 false。
func isHttp(b []byte) bool {
	// 定义有效的 HTTP 方法前缀
	validMethods := [][]byte{
		[]byte("GET"), []byte("HEA"), []byte("POS"),
		[]byte("PUT"), []byte("DEL"), []byte("CON"),
		[]byte("OPT"), []byte("TRA"), []byte("PAT"),
	}

	// 遍历所有有效的 HTTP 方法前缀
	for _, vm := range validMethods {
		// 如果数据以某个 HTTP 方法前缀开头，返回 true
		if bytes.HasPrefix(b, vm) {
			return true
		}
	}

	// 如果数据不符合任何 HTTP 方法前缀，返回 false
	return false
}

// determineProtocol 确定连接的协议类型。
// 参数：
// - conn: 要确定协议类型的网络连接。
// 返回值：
// - net.Conn: 包装后的连接对象，方便后续处理。
// - protocols.Type: 确定的协议类型。
// - error: 如果无法确定协议类型，返回错误；否则返回 nil。
func (m *Multiplexer) determineProtocol(conn net.Conn) (net.Conn, protocols.Type, error) {
	// 创建一个大小为 14 字节的缓冲区，用于读取连接的头部数据
	header := make([]byte, 14)
	// 从连接中读取最多 14 字节的数据
	n, err := conn.Read(header)
	if err != nil {
		// 如果读取失败，关闭连接并返回错误
		conn.Close()
		return nil, "", fmt.Errorf("failed to read header: %s", err)
	}

	// 创建一个 bufferedConn 对象，用于包装原始连接和读取到的头部数据
	c := &bufferedConn{prefix: header[:n], conn: conn}

	// 根据头部数据判断协议类型
	if bytes.HasPrefix(header, []byte{'R', 'A', 'W'}) {
		// 如果头部以 "RAW" 开头，判定为 TCP 下载协议
		return c, protocols.TCPDownload, nil
	}

	if bytes.HasPrefix(header, []byte{0x16}) {
		// 如果头部以 0x16 开头，判定为 TLS 协议
		return c, protocols.TLS, nil
	}

	if bytes.HasPrefix(header, []byte{'S', 'S', 'H'}) {
		// 如果头部以 "SSH" 开头，判定为 C2 协议
		return c, protocols.C2, nil
	}

	// 如果头部数据符合 HTTP 请求格式
	if isHttp(header) {
		// 如果是 WebSocket 请求
		if bytes.HasPrefix(header, []byte("GET /ws")) {
			return c, protocols.Websockets, nil
		}

		// 如果是 HTTP 推送请求
		if bytes.HasPrefix(header, []byte("HEAD /push")) || bytes.HasPrefix(header, []byte("GET /push")) || bytes.HasPrefix(header, []byte("POST /push")) {
			return c, protocols.HTTP, nil
		}

		// 如果是普通的 HTTP 请求，判定为 HTTP 下载协议
		return c, protocols.HTTPDownload, nil
	}

	// 如果无法识别协议类型，关闭连接并返回错误
	conn.Close()
	return nil, "", errors.New("unknown protocol: " + string(header[:n]))
}

// getProtoListener 根据协议类型获取对应的监听器。
// 参数：
// - proto: 协议类型（protocols.Type）。
// 返回值：
// - net.Listener: 对应协议类型的监听器。
func (m *Multiplexer) getProtoListener(proto protocols.Type) net.Listener {
	// 从 result 映射中查找指定协议类型的监听器
	ml, ok := m.result[proto]
	if !ok {
		// 如果未找到对应的监听器，抛出 panic
		panic("Unknown protocol passed: " + string(proto))
	}

	// 返回找到的监听器
	return ml
}

// unwrapTransports 对传入的网络连接进行协议解封装，确定其最终的协议类型。
// 参数：
// - conn: 要解封装的网络连接。
// 返回值：
// - net.Conn: 解封装后的连接对象。
// - protocols.Type: 解封装后的协议类型。
// - error: 如果解封装失败，返回错误；否则返回 nil。
func (m *Multiplexer) unwrapTransports(conn net.Conn) (net.Conn, protocols.Type, error) {
	// 设置连接的超时时间为 2 秒
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// 调用 determineProtocol 方法，初步确定连接的协议类型
	var proto protocols.Type
	conn, proto, err := m.determineProtocol(conn)
	if err != nil {
		// 如果初步确定失败，返回错误
		return nil, protocols.Invalid, fmt.Errorf("initial determination: %s", err)
	}

	// 清除连接的超时时间
	conn.SetDeadline(time.Time{})

	// 如果配置中启用了 TLS，并且初步确定的协议是 TLS
	if m.config.TLS && proto == protocols.TLS {
		// 如果尚未配置 TLS 配置对象
		if m.config.tlsConfig == nil {
			// 创建一个 TLS 配置对象
			tlsConfig := &tls.Config{
				PreferServerCipherSuites: true, // 优先使用服务器端的加密套件
				CurvePreferences: []tls.CurveID{
					tls.CurveP256, // 椭圆曲线 P-256
					tls.X25519,    // Go 1.8 及以上版本支持的椭圆曲线
				},
				MinVersion: tls.VersionTLS12, // 最低支持的 TLS 版本为 TLS 1.2
			}

			// 如果配置了 TLS 证书路径
			if m.config.TLSCertPath != "" {
				// 加载 TLS 证书和私钥
				cert, err := tls.LoadX509KeyPair(m.config.TLSCertPath, m.config.TLSKeyPath)
				if err != nil {
					// 如果加载证书失败，返回错误
					return nil, protocols.Invalid, fmt.Errorf("TLS is enabled but loading certs/key failed: %s, err: %s", m.config.TLSCertPath, err)
				}

				// 将加载的证书添加到 TLS 配置中
				tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
			} else {
				// 如果未配置证书路径，则生成自签名证书
				cert, err := genX509KeyPair(m.config.AutoTLSCommonName)
				if err != nil {
					// 如果生成证书失败，返回错误
					return nil, protocols.Invalid, fmt.Errorf("TLS is enabled but generating certs/key failed: %s", err)
				}
				// 将生成的证书添加到 TLS 配置中
				tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
			}

			// 将 TLS 配置对象存储到多路复用器的配置中
			m.config.tlsConfig = tlsConfig
		}

		// 使用 TLS 配置对象对连接进行 TLS 服务端处理
		c := tls.Server(conn, m.config.tlsConfig)
		// 执行 TLS 握手
		err := c.Handshake()
		if err != nil {
			// 如果握手失败，关闭连接并返回错误
			conn.Close()
			return nil, protocols.Invalid, fmt.Errorf("multiplexing failed (tls handshake): err: %s", err)
		}

		// 由于解封装了 TLS，需要再次确定内部协议类型
		conn, proto, err = m.determineProtocol(c)
		if err != nil {
			// 如果再次确定失败，返回错误
			return nil, protocols.Invalid, fmt.Errorf("error determining functional protocol: %s", err)
		}
	}

	// 根据最终确定的协议类型进行进一步处理
	switch proto {
	case protocols.Websockets:
		// 如果是 WebSocket 协议，调用 unwrapWebsockets 方法进行解封装
		return m.unwrapWebsockets(conn)
	case protocols.HTTP:
		// 如果是 HTTP 协议，直接返回连接对象和协议类型
		// 注意：HTTP 协议不会进行进一步解封装，因为它可能包含多个连接
		return conn, protocols.HTTP, nil
	default:
		// 如果协议类型是完全解封装后的类型（如 TCP 下载或 C2 协议），直接返回
		if protocols.FullyUnwrapped(proto) {
			return conn, proto, nil
		}
	}

	// 如果经过解封装后仍未找到可用的协议类型，返回错误
	return nil, protocols.Invalid, fmt.Errorf("after unwrapping transports, nothing useable was found: %s", proto)
}

// unwrapWebsockets 对传入的网络连接进行 WebSocket 协议解封装。
// 参数：
// - conn: 要解封装的网络连接。
// 返回值：
// - net.Conn: 解封装后的连接对象。
// - protocols.Type: 解封装后的协议类型。
// - error: 如果解封装失败，返回错误；否则返回 nil。
func (m *Multiplexer) unwrapWebsockets(conn net.Conn) (net.Conn, protocols.Type, error) {
	// 创建一个 HTTP 服务复用器
	wsHttp := http.NewServeMux()
	// 创建一个通道，用于接收解封装后的 WebSocket 连接
	wsConnChan := make(chan net.Conn, 1)

	// 创建一个 WebSocket 服务器
	wsServer := websocket.Server{
		Config: websocket.Config{}, // 使用默认配置

		// 禁用握手验证（因为这是 SSH 连接，不需要进行 Origin 验证）
		Handshake: nil,
		Handler: func(c *websocket.Conn) {
			// 设置 WebSocket 连接的负载类型为二进制帧
			// 参考：https://github.com/golang/go/issues/7350
			c.PayloadType = websocket.BinaryFrame

			// 创建一个 WebSocket 包装器
			wsW := websocketWrapper{
				wsConn:  c,                      // WebSocket 连接
				tcpConn: conn,                   // 原始 TCP 连接
				done:    make(chan interface{}), // 用于同步的通道
			}

			// 将包装后的 WebSocket 连接发送到通道中
			wsConnChan <- &wsW

			// 等待 WebSocket 连接关闭
			<-wsW.done
		},
	}

	// 将 WebSocket 服务器绑定到 "/ws" 路径
	wsHttp.Handle("/ws", wsServer)

	// 启动一个协程，使用单连接监听器运行 HTTP 服务器
	go http.Serve(&singleConnListener{conn: conn}, wsHttp)

	// 等待 WebSocket 连接解封装完成或超时
	select {
	case wsConn := <-wsConnChan:
		// 确定 WebSocket 连接中承载的协议类型
		result, proto, err := m.determineProtocol(wsConn)
		if err != nil {
			// 如果无法确定协议类型，关闭连接并返回错误
			conn.Close()
			return nil, protocols.Invalid, fmt.Errorf("failed to determine protocol being carried by ws: %s", err)
		}

		// 检查是否解封装到了完全解封装的协议类型（如 C2 或下载协议）
		if !protocols.FullyUnwrapped(proto) {
			conn.Close()
			return nil, protocols.Invalid, errors.New("after unwrapping websockets found another protocol to unwrap (not control channel or download), does not support infinite protocol nesting")
		}

		// 返回解封装后的连接和协议类型
		return result, proto, nil

	case <-time.After(2 * time.Second):
		// 如果 WebSocket 解封装超时，关闭连接并返回错误
		conn.Close()
		return nil, protocols.Invalid, errors.New("multiplexing failed: websockets took too long to negotiate")
	}
}

// ControlRequests 返回用于控制请求的监听器。
// 返回值：
// - net.Listener: 控制请求的监听器。
func (m *Multiplexer) ControlRequests() net.Listener {
	return m.getProtoListener(protocols.C2)
}

// HTTPDownloadRequests 返回用于 HTTP 下载请求的监听器。
// 返回值：
// - net.Listener: HTTP 下载请求的监听器。
func (m *Multiplexer) HTTPDownloadRequests() net.Listener {
	return m.getProtoListener(protocols.HTTPDownload)
}

// TCPDownloadRequests 返回用于 TCP 下载请求的监听器。
// 返回值：
// - net.Listener: TCP 下载请求的监听器。
func (m *Multiplexer) TCPDownloadRequests() net.Listener {
	return m.getProtoListener(protocols.TCPDownload)
}
