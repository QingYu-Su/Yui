package mux

import (
	"net"  // 导入用于处理网络连接的包
	"time" // 导入用于处理时间相关的操作
)

// bufferedConn 是一个包装类型，用于扩展 net.Conn 的功能。
// 它在读取数据时会先从一个前缀缓冲区读取数据，然后再从底层连接读取。
type bufferedConn struct {
	prefix []byte   // 前缀缓冲区，用于存储额外的数据
	conn   net.Conn // 底层的网络连接
}

// Read 方法实现了 io.Reader 接口，用于从连接中读取数据。
// 它会先从 prefix 缓冲区读取数据，如果缓冲区数据不足，再从底层连接读取。
func (bc *bufferedConn) Read(b []byte) (n int, err error) {
	if len(bc.prefix) > 0 {
		// 如果 prefix 缓冲区中有数据，先从缓冲区读取
		n = copy(b, bc.prefix) // 将 prefix 中的数据复制到目标缓冲区 b 中

		bc.prefix = bc.prefix[n:] // 更新 prefix 缓冲区，移除已读取的部分

		// 如果目标缓冲区 b 还有剩余空间，继续从底层连接读取数据
		if len(b)-n > 0 {
			var actualRead int
			actualRead, err = bc.conn.Read(b[n:]) // 从底层连接读取数据
			n += actualRead                       // 更新已读取的总字节数
		}

		return n, err // 返回已读取的字节数和可能的错误
	}

	// 如果 prefix 缓冲区为空，直接从底层连接读取数据
	return bc.conn.Read(b)
}

// Write 方法实现了 io.Writer 接口，用于将数据写入连接。
// 它直接调用底层连接的 Write 方法。
func (bc *bufferedConn) Write(b []byte) (n int, err error) {
	return bc.conn.Write(b) // 将数据写入底层连接
}

// Close 方法用于关闭连接。
// 它直接调用底层连接的 Close 方法。
func (bc *bufferedConn) Close() error {
	return bc.conn.Close() // 关闭底层连接
}

// LocalAddr 方法返回本地地址。
// 它直接调用底层连接的 LocalAddr 方法。
func (bc *bufferedConn) LocalAddr() net.Addr {
	return bc.conn.LocalAddr() // 返回底层连接的本地地址
}

// RemoteAddr 方法返回远程地址。
// 它直接调用底层连接的 RemoteAddr 方法。
func (bc *bufferedConn) RemoteAddr() net.Addr {
	return bc.conn.RemoteAddr() // 返回底层连接的远程地址
}

// SetDeadline 方法设置连接的读写截止时间。
// 它直接调用底层连接的 SetDeadline 方法。
func (bc *bufferedConn) SetDeadline(t time.Time) error {
	return bc.conn.SetDeadline(t) // 设置底层连接的截止时间
}

// SetReadDeadline 方法设置连接的读取截止时间。
// 它直接调用底层连接的 SetReadDeadline 方法。
func (bc *bufferedConn) SetReadDeadline(t time.Time) error {
	return bc.conn.SetReadDeadline(t) // 设置底层连接的读取截止时间
}

// SetWriteDeadline 方法设置连接的写入截止时间。
// 它直接调用底层连接的 SetWriteDeadline 方法。
func (bc *bufferedConn) SetWriteDeadline(t time.Time) error {
	return bc.conn.SetWriteDeadline(t) // 设置底层连接的写入截止时间
}
