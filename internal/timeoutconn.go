package internal // 定义包名为 internal，通常用于项目内部的私有包

import (
	"net"  // 导入 net 包，用于处理网络连接
	"time" // 导入 time 包，用于处理时间相关的操作
)

// TimeoutConn 是一个自定义的结构体，用于包装 net.Conn 并添加超时功能。
// 它包含一个 net.Conn 类型的字段，用于存储底层的网络连接，
// 以及一个 Timeout 字段，用于设置读写操作的超时时间。
type TimeoutConn struct {
	net.Conn               // 嵌入 net.Conn，继承其方法和属性
	Timeout  time.Duration // 定义超时时间，类型为 time.Duration
}

// Read 方法用于实现超时控制的读取操作。
// 它会根据 Timeout 字段的值，设置连接的读取超时时间。
// 如果 Timeout 不为 0，则调用 SetDeadline 方法设置超时时间。
// 然后调用底层 net.Conn 的 Read 方法进行实际的读取操作。
func (c *TimeoutConn) Read(b []byte) (int, error) {
	// 如果设置了超时时间，则设置连接的读取截止时间
	if c.Timeout != 0 {
		c.Conn.SetDeadline(time.Now().Add(c.Timeout))
	}
	// 调用底层 net.Conn 的 Read 方法进行读取操作
	return c.Conn.Read(b)
}

// Write 方法用于实现超时控制的写入操作。
// 它会根据 Timeout 字段的值，设置连接的写入超时时间。
// 如果 Timeout 不为 0，则调用 SetDeadline 方法设置超时时间。
// 然后调用底层 net.Conn 的 Write 方法进行实际的写入操作。
func (c *TimeoutConn) Write(b []byte) (int, error) {
	// 如果设置了超时时间，则设置连接的写入截止时间
	if c.Timeout != 0 {
		c.Conn.SetDeadline(time.Now().Add(c.Timeout))
	}
	// 调用底层 net.Conn 的 Write 方法进行写入操作
	return c.Conn.Write(b)
}
