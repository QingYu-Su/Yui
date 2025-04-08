package mux

import (
	"io"   // 导入用于处理输入输出的包
	"net"  // 导入用于处理网络连接的包
	"sync" // 导入用于同步操作的包
)

// singleConnListener 是一个简单的网络监听器，用于管理单个连接。
// 它实现了 net.Listener 接口，但只允许接受一次连接。
type singleConnListener struct {
	conn net.Conn   // 存储的网络连接
	done bool       // 标志位，表示是否已经接受过连接
	l    sync.Mutex // 互斥锁，用于保护 done 标志位的线程安全
}

// Accept 方法用于接受新的连接。
// 如果已经接受过连接或监听器已关闭，返回错误。
func (l *singleConnListener) Accept() (net.Conn, error) {
	l.l.Lock()         // 加锁，确保线程安全
	defer l.l.Unlock() // 确保在函数返回时释放锁

	if l.done { // 如果已经接受过连接
		return nil, io.ErrClosedPipe // 返回错误
	}

	l.done = true // 设置已接受连接的标志

	return l.conn, nil // 返回存储的连接
}

// Addr 方法返回监听器的网络地址。
// 这里返回的是存储连接的远程地址。
func (l *singleConnListener) Addr() net.Addr {
	return l.conn.RemoteAddr() // 返回存储连接的远程地址
}

// Close 方法关闭监听器。
// 这个实现中，Close 方法不执行任何操作，只是满足 net.Listener 接口的要求。
func (l *singleConnListener) Close() error {
	return nil // 返回 nil，表示关闭成功
}
