package mux

import (
	"crypto/rand"  // 导入用于生成随机数据的包
	"encoding/hex" // 导入用于将字节数据编码为十六进制字符串的包
	"errors"       // 导入用于处理错误的包
	"io"           // 导入用于处理输入输出的包
	"net"          // 导入用于处理网络连接的包
	"time"         // 导入用于处理时间相关的操作
)

// ErrClosed 是一个预定义的错误，表示分片收集器已经被关闭。
var ErrClosed = errors.New("fragment collector has been closed")

// maxBuffer 定义了缓冲区的最大大小。
const maxBuffer = 8096

// fragmentedConnection 是一个用于处理分片连接的结构体。
// 它通过两个同步缓冲区（读缓冲区和写缓冲区）管理数据的读写。
type fragmentedConnection struct {
	done chan interface{} // 用于通知关闭操作的通道

	readBuffer  *SyncBuffer // 读缓冲区
	writeBuffer *SyncBuffer // 写缓冲区

	localAddr  net.Addr // 本地地址
	remoteAddr net.Addr // 远程地址

	isDead *time.Timer // 用于检测连接是否超时的定时器

	onClose func() // 关闭时的回调函数
}

// NewFragmentCollector 函数用于创建一个新的分片连接。
// 参数：
//   - localAddr：本地地址
//   - remoteAddr：远程地址
//   - onClosed：关闭时的回调函数
//
// 返回值：
//   - *fragmentedConnection：创建的分片连接对象
//   - string：分片连接的唯一标识符
//   - error：如果发生错误，返回错误信息
func NewFragmentCollector(localAddr net.Addr, remoteAddr net.Addr, onClosed func()) (*fragmentedConnection, string, error) {
	fc := &fragmentedConnection{
		done: make(chan interface{}), // 创建一个关闭通知通道

		readBuffer:  NewSyncBuffer(maxBuffer), // 创建读缓冲区
		writeBuffer: NewSyncBuffer(maxBuffer), // 创建写缓冲区
		localAddr:   localAddr,                // 设置本地地址
		remoteAddr:  remoteAddr,               // 设置远程地址
		onClose:     onClosed,                 // 设置关闭回调函数
	}

	// 设置超时检测定时器
	// 如果在 2 秒内没有读写操作，认为连接已死亡并调用 Close 方法
	fc.isDead = time.AfterFunc(2*time.Second, func() {
		fc.Close()
	})

	// 生成一个随机的唯一标识符
	randomData := make([]byte, 16)
	_, err := rand.Read(randomData)
	if err != nil {
		return nil, "", err // 如果生成随机数据失败，返回错误
	}

	id := hex.EncodeToString(randomData) // 将随机数据编码为十六进制字符串作为唯一标识符

	return fc, id, nil // 返回分片连接对象和唯一标识符
}

// IsAlive 方法用于重置超时定时器，表示连接仍然活跃。
func (fc *fragmentedConnection) IsAlive() {
	fc.isDead.Reset(2 * time.Second) // 重置超时定时器
}

// Read 方法用于从分片连接中读取数据。
// 参数：
//   - b：目标缓冲区
//
// 返回值：
//   - int：读取的字节数
//   - error：如果发生错误，返回错误信息
func (fc *fragmentedConnection) Read(b []byte) (n int, err error) {
	select {
	case <-fc.done: // 检查是否已经关闭
		return 0, io.EOF // 如果已关闭，返回 EOF
	default:
	}

	// 从读缓冲区中读取数据
	n, err = fc.readBuffer.BlockingRead(b)
	return
}

// Write 方法用于向分片连接中写入数据。
// 参数：
//   - b：要写入的数据
//
// 返回值：
//   - int：写入的字节数
//   - error：如果发生错误，返回错误信息
func (fc *fragmentedConnection) Write(b []byte) (n int, err error) {
	select {
	case <-fc.done: // 检查是否已经关闭
		return 0, io.EOF // 如果已关闭，返回 EOF
	default:
	}

	// 向写缓冲区中写入数据
	n, err = fc.writeBuffer.BlockingWrite(b)
	return
}

// Close 方法用于关闭分片连接。
// 它会关闭读写缓冲区，并调用关闭回调函数。
func (fc *fragmentedConnection) Close() error {
	// 关闭读写缓冲区
	fc.writeBuffer.Close()
	fc.readBuffer.Close()

	select {
	case <-fc.done: // 检查是否已经关闭
	default:
		close(fc.done) // 关闭通知通道（该通道无法再次写入，只能被读，而且只会返回零值，用于通知所有协程）
		fc.onClose()   // 调用关闭回调函数
	}

	return nil
}

// LocalAddr 方法返回分片连接的本地地址。
func (fc *fragmentedConnection) LocalAddr() net.Addr {
	return fc.localAddr
}

// RemoteAddr 方法返回分片连接的远程地址。
func (fc *fragmentedConnection) RemoteAddr() net.Addr {
	return fc.remoteAddr
}

// SetDeadline 方法是一个空实现，用于满足 net.Conn 接口的要求。
func (fc *fragmentedConnection) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline 方法是一个空实现，用于满足 net.Conn 接口的要求。
func (fc *fragmentedConnection) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline 方法是一个空实现，用于满足 net.Conn 接口的要求。
func (fc *fragmentedConnection) SetWriteDeadline(t time.Time) error {
	return nil
}
