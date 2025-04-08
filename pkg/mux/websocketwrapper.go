package mux

import (
	"net"  // 导入用于处理网络连接的包
	"time" // 导入用于处理时间的包

	"golang.org/x/net/websocket" // 导入用于处理 WebSocket 的包
)

// websocketWrapper 是一个包装器，将 WebSocket 连接包装成一个符合 net.Conn 接口的对象。
// 它允许 WebSocket 连接像普通的 TCP 连接一样被使用。
type websocketWrapper struct {
	wsConn  *websocket.Conn  // WebSocket 连接
	tcpConn net.Conn         // 原始的 TCP 连接
	done    chan interface{} // 用于通知连接关闭的通道
}

// Read 方法从 WebSocket 连接中读取数据。
// 参数：
//   - b：目标缓冲区
//
// 返回值：
//   - n：读取的字节数
//   - err：如果发生错误，返回错误信息
func (ww *websocketWrapper) Read(b []byte) (n int, err error) {
	n, err = ww.wsConn.Read(b) // 从 WebSocket 连接读取数据
	if err != nil {
		ww.done <- true // 如果读取失败，通知关闭
	}
	return n, err
}

// Write 方法向 WebSocket 连接中写入数据。
// 参数：
//   - b：要写入的数据
//
// 返回值：
//   - n：写入的字节数
//   - err：如果发生错误，返回错误信息
func (ww *websocketWrapper) Write(b []byte) (n int, err error) {
	n, err = ww.wsConn.Write(b) // 向 WebSocket 连接写入数据
	if err != nil {
		ww.done <- true // 如果写入失败，通知关闭
	}
	return
}

// Close 方法关闭 WebSocket 连接。
// 返回值：
//   - error：如果发生错误，返回错误信息
func (ww *websocketWrapper) Close() error {
	err := ww.wsConn.Close() // 关闭 WebSocket 连接
	ww.done <- true          // 通知关闭
	return err
}

// LocalAddr 方法返回 WebSocket 连接的本地地址。
// 返回值：
//   - net.Addr：本地地址
func (ww *websocketWrapper) LocalAddr() net.Addr {
	return ww.tcpConn.LocalAddr() // 返回原始 TCP 连接的本地地址
}

// RemoteAddr 方法返回 WebSocket 连接的远程地址。
// 返回值：
//   - net.Addr：远程地址
func (ww *websocketWrapper) RemoteAddr() net.Addr {
	return ww.tcpConn.RemoteAddr() // 返回原始 TCP 连接的远程地址
}

// SetDeadline 方法设置 WebSocket 连接的读写截止时间。
// 参数：
//   - t：截止时间
//
// 返回值：
//   - error：如果发生错误，返回错误信息
func (ww *websocketWrapper) SetDeadline(t time.Time) error {
	return ww.wsConn.SetDeadline(t) // 设置 WebSocket 连接的截止时间
}

// SetReadDeadline 方法设置 WebSocket 连接的读取截止时间。
// 参数：
//   - t：截止时间
//
// 返回值：
//   - error：如果发生错误，返回错误信息
func (ww *websocketWrapper) SetReadDeadline(t time.Time) error {
	return ww.wsConn.SetReadDeadline(t) // 设置 WebSocket 连接的读取截止时间
}

// SetWriteDeadline 方法设置 WebSocket 连接的写入截止时间。
// 参数：
//   - t：截止时间
//
// 返回值：
//   - error：如果发生错误，返回错误信息
func (ww *websocketWrapper) SetWriteDeadline(t time.Time) error {
	return ww.wsConn.SetWriteDeadline(t) // 设置 WebSocket 连接的写入截止时间
}
