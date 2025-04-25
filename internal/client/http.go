package client

import (
	"bytes"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	mathrand "math/rand"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/QingYu-Su/Yui/internal/client/keys"
	"github.com/QingYu-Su/Yui/pkg/mux"
)

// HTTPConn 表示一个基于HTTP协议的连接封装
// 该结构体实现了net.Conn接口，用于在HTTP协议上模拟原始TCP连接
type HTTPConn struct {
	ID      string // 连接唯一标识符
	address string // 远程服务器地址

	done chan interface{} // 用于通知连接关闭的通道

	readBuffer *mux.SyncBuffer // 线程安全的读缓冲区

	// start 用于缓存清除中间件代理的随机起始值
	// 通过随机值避免代理缓存问题
	start int

	// client 是底层HTTP客户端，用于实际发送请求
	client *http.Client
}

// NewHTTPConn 创建一个新的HTTP连接封装
// 参数:
//
//	address - 服务器地址
//	connector - 底层连接创建函数
//
// 返回值:
//
//	*HTTPConn - 创建的HTTP连接对象
//	error - 如果创建失败则返回错误
func NewHTTPConn(address string, connector func() (net.Conn, error)) (*HTTPConn, error) {
	// 初始化HTTPConn结构体
	result := &HTTPConn{
		done:       make(chan interface{}),  // 创建关闭通知通道
		readBuffer: mux.NewSyncBuffer(8096), // 创建8KB的线程安全缓冲区
		address:    address,                 // 设置服务器地址
		start:      mathrand.Int(),          // 初始化随机起始值(用于缓存清除)
	}

	// 配置HTTP客户端
	result.client = &http.Client{
		Transport: &http.Transport{
			// 自定义拨号函数，使用传入的connector创建底层连接
			Dial: func(network, addr string) (net.Conn, error) {
				return connector()
			},
			// 跳过TLS证书验证
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		// 禁止自动重定向
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 获取SSH私钥并提取公钥
	s, err := keys.GetPrivateKey()
	if err != nil {
		return nil, err
	}
	publicKeyBytes := s.PublicKey().Marshal()

	// 发送HEAD请求初始化连接
	resp, err := result.client.Head(address + "/push?key=" + hex.EncodeToString(publicKeyBytes))
	if err != nil {
		return nil, fmt.Errorf("连接失败 %s/push?key=%s, 错误: %s",
			address, hex.EncodeToString(publicKeyBytes), err)
	}
	resp.Body.Close()

	// 检查服务器响应状态码(期望307临时重定向)
	if resp.StatusCode != http.StatusTemporaryRedirect {
		return nil, fmt.Errorf("服务器拒绝建立会话: 期望 %d 实际 %d",
			http.StatusTemporaryRedirect, resp.StatusCode)
	}

	// 从响应Cookie中获取会话ID
	found := false
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "NID" {
			result.ID = cookie.Value
			found = true
			break
		}
	}

	if !found {
		return nil, errors.New("服务器未返回会话ID")
	}

	// 启动后台读取循环
	go result.startReadLoop()

	return result, nil
}

// startReadLoop 启动后台读取循环，持续从服务器获取数据
func (c *HTTPConn) startReadLoop() {
	for {
		select {
		case <-c.done:
			// 收到关闭信号，退出循环
			return
		default:
		}

		// 发送GET请求获取数据(包含缓存清除参数)
		resp, err := c.client.Get(c.address + "/push/" + strconv.Itoa(c.start) + "?id=" + c.ID)
		if err != nil {
			log.Println("获取数据错误: ", err)
			c.Close()
			return
		}

		// 将响应体数据拷贝到读缓冲区
		_, err = io.Copy(c.readBuffer, resp.Body)
		if err != nil {
			log.Println("拷贝数据错误: ", err)
			c.Close()
			return
		}

		resp.Body.Close()

		// 递增起始值，避免代理缓存
		c.start++

		// 短暂休眠避免CPU占用过高
		time.Sleep(10 * time.Millisecond)
	}
}

// Read 从连接中读取数据到指定缓冲区
// 参数:
//
//	b - 用于存储读取数据的字节切片
//
// 返回值:
//
//	n - 实际读取的字节数
//	err - 错误信息(如连接已关闭)
func (c *HTTPConn) Read(b []byte) (n int, err error) {
	// 检查连接是否已关闭
	select {
	case <-c.done:
		return 0, io.EOF // 连接已关闭，返回EOF
	default:
	}

	// 从线程安全缓冲区中阻塞读取数据
	n, err = c.readBuffer.BlockingRead(b)

	return
}

// Write 将数据写入连接
// 参数:
//
//	b - 要写入的字节切片
//
// 返回值:
//
//	n - 实际写入的字节数(总是全部写入)
//	err - 错误信息(如连接已关闭或写入失败)
func (c *HTTPConn) Write(b []byte) (n int, err error) {
	// 检查连接是否已关闭
	select {
	case <-c.done:
		return 0, io.EOF // 连接已关闭，返回EOF
	default:
	}

	// 通过HTTP POST发送数据到服务器
	resp, err := c.client.Post(
		c.address+"/push?id="+c.ID, // 目标URL包含会话ID
		"application/octet-stream", // 使用二进制流内容类型
		bytes.NewBuffer(b))         // 数据缓冲区

	if err != nil {
		c.Close() // 发生错误时关闭连接
		return 0, err
	}
	resp.Body.Close() // 确保响应体被关闭

	return len(b), nil // 总是返回全部写入
}

// Close 关闭连接并释放资源
// 返回值:
//
//	error - 总是返回nil
func (c *HTTPConn) Close() error {
	// 关闭读缓冲区
	c.readBuffer.Close()

	// 安全关闭done通道(避免重复关闭)
	select {
	case <-c.done: // 如果已经关闭
		return nil
	default:
		close(c.done) // 首次关闭
	}

	return nil
}

// LocalAddr 返回本地网络地址(固定为127.0.0.1)
// 返回值:
//
//	net.Addr - 本地TCP地址
func (c *HTTPConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Zone: ""}
}

// RemoteAddr 返回远程网络地址(固定为127.0.0.1)
// 返回值:
//
//	net.Addr - 远程TCP地址
func (c *HTTPConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Zone: ""}
}

// SetDeadline 设置读写截止时间(未实现)
func (c *HTTPConn) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline 设置读截止时间(未实现)
func (c *HTTPConn) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline 设置写截止时间(未实现)
func (c *HTTPConn) SetWriteDeadline(t time.Time) error {
	return nil
}
