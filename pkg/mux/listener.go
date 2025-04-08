package mux

import (
	"errors" // 导入用于处理错误的包
	"net"    // 导入用于处理网络连接的包

	"github.com/QingYu-Su/Yui/pkg/mux/protocols" // 导入 protocols 包，用于处理协议类型
)

// multiplexerListener 是一个自定义的网络监听器，用于管理网络连接。
// 它通过一个通道接收连接，并支持关闭操作。
type multiplexerListener struct {
	addr        net.Addr       // 监听器的网络地址
	connections chan net.Conn  // 用于接收连接的通道
	closed      bool           // 标志位，表示监听器是否已关闭
	protocol    protocols.Type // 监听器使用的协议类型
}

// newMultiplexerListener 函数用于创建一个新的 multiplexerListener 实例。
// 参数：
//   - addr：监听器的网络地址
//   - protocol：监听器使用的协议类型
//
// 返回值：
//   - *multiplexerListener：创建的监听器实例
func newMultiplexerListener(addr net.Addr, protocol protocols.Type) *multiplexerListener {
	return &multiplexerListener{
		addr:        addr,                // 设置监听器的网络地址
		connections: make(chan net.Conn), // 创建一个通道用于接收连接
		protocol:    protocol,            // 设置监听器使用的协议类型
	}
}

// Accept 方法用于接收新的连接。
// 如果监听器已关闭，返回错误。
func (ml *multiplexerListener) Accept() (net.Conn, error) {
	if ml.closed { // 如果监听器已关闭
		return nil, errors.New("Accept on closed listener") // 返回错误
	}
	return <-ml.connections, nil // 从通道接收连接并返回
}

// Close 方法关闭监听器。
// 任何阻塞的 Accept 操作将被解除阻塞，并返回错误。
func (ml *multiplexerListener) Close() error {
	if !ml.closed { // 如果监听器未关闭
		ml.closed = true      // 设置关闭标志
		close(ml.connections) // 关闭连接通道
	}
	return nil
}

// Addr 方法返回监听器的网络地址。
// 如果监听器已关闭，返回 nil。
func (ml *multiplexerListener) Addr() net.Addr {
	if ml.closed { // 如果监听器已关闭
		return nil // 返回 nil
	}
	return ml.addr // 返回监听器的网络地址
}
