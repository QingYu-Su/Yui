package mux

import (
	"bytes" // 导入用于操作字节缓冲区的包
	"io"    // 导入用于处理输入输出的包
	"sync"  // 导入用于同步操作的包
)

// SyncBuffer 是一个线程安全的缓冲区，支持阻塞读写操作。
// 它基于 bytes.Buffer 实现，通过 sync.Mutex 和 sync.Cond 提供线程安全的读写控制。
type SyncBuffer struct {
	bb *bytes.Buffer // 内部的字节缓冲区

	sync.Mutex // 内嵌互斥锁，用于保护缓冲区的线程安全

	rwait sync.Cond // 读等待条件变量
	wwait sync.Cond // 写等待条件变量

	maxLength int // 缓冲区的最大长度

	isClosed bool // 标志位，表示缓冲区是否已关闭
}

// BlockingRead 方法从内部缓冲区读取数据，如果缓冲区为空，则阻塞等待，直到有数据可读或缓冲区关闭。
// 参数：
//   - p：目标缓冲区
//
// 返回值：
//   - n：读取的字节数
//   - err：如果发生错误，返回错误信息
func (sb *SyncBuffer) BlockingRead(p []byte) (n int, err error) {
	sb.Lock()               // 加锁，确保线程安全
	defer sb.wwait.Signal() // 写操作完成后通知等待的写操作
	defer sb.Unlock()       // 确保在函数返回时释放锁

	if sb.isClosed { // 如果缓冲区已关闭
		return 0, ErrClosed // 返回关闭错误
	}

	n, err = sb.bb.Read(p) // 从内部缓冲区读取数据
	if err == io.EOF {     // 如果缓冲区为空
		for err == io.EOF { // 阻塞等待，直到有数据可读
			sb.wwait.Signal() // 通知等待的写操作
			sb.rwait.Wait()   // 等待读操作

			if sb.isClosed { // 如果缓冲区已关闭
				return 0, ErrClosed // 返回关闭错误
			}

			n, err = sb.bb.Read(p) // 再次尝试读取数据
		}
		return
	}

	return
}

// Read 方法从内部缓冲区读取数据，非阻塞。
// 参数：
//   - p：目标缓冲区
//
// 返回值：
//   - n：读取的字节数
//   - err：如果发生错误，返回错误信息
func (sb *SyncBuffer) Read(p []byte) (n int, err error) {
	sb.Lock()               // 加锁，确保线程安全
	defer sb.wwait.Signal() // 写操作完成后通知等待的写操作
	defer sb.Unlock()       // 确保在函数返回时释放锁

	return sb.bb.Read(p) // 从内部缓冲区读取数据
}

// BlockingWrite 方法向内部缓冲区写入数据，如果缓冲区已满，则阻塞等待，直到缓冲区有空间。
// 参数：
//   - p：要写入的数据
//
// 返回值：
//   - n：写入的字节数
//   - err：如果发生错误，返回错误信息
func (sb *SyncBuffer) BlockingWrite(p []byte) (n int, err error) {
	sb.Lock()               // 加锁，确保线程安全
	defer sb.rwait.Signal() // 读操作完成后通知等待的读操作
	defer sb.Unlock()       // 确保在函数返回时释放锁

	if sb.isClosed { // 如果缓冲区已关闭
		return 0, ErrClosed // 返回关闭错误
	}

	n, err = sb.bb.Write(p) // 向内部缓冲区写入数据
	if err != nil {         // 如果写入失败
		return 0, err // 返回错误
	}
	for {
		sb.rwait.Signal() // 通知等待的读操作
		sb.wwait.Wait()   // 等待写操作

		if sb.isClosed { // 如果缓冲区已关闭
			return 0, ErrClosed // 返回关闭错误
		}

		if sb.bb.Len() == 0 { // 如果缓冲区为空
			return len(p), nil // 返回写入的字节数
		}
	}
}

// Write 方法向内部缓冲区写入数据，非阻塞。
// 参数：
//   - p：要写入的数据
//
// 返回值：
//   - n：写入的字节数
//   - err：如果发生错误，返回错误信息
func (sb *SyncBuffer) Write(p []byte) (n int, err error) {
	sb.Lock()               // 加锁，确保线程安全
	defer sb.rwait.Signal() // 读操作完成后通知等待的读操作
	defer sb.Unlock()       // 确保在函数返回时释放锁

	if sb.isClosed { // 如果缓冲区已关闭
		return 0, ErrClosed // 返回关闭错误
	}

	return sb.bb.Write(p) // 向内部缓冲区写入数据
}

// Len 方法返回内部缓冲区的当前长度。
// 返回值：
//   - int：缓冲区的当前长度
func (sb *SyncBuffer) Len() int {
	sb.Lock()         // 加锁，确保线程安全
	defer sb.Unlock() // 确保在函数返回时释放锁

	return sb.bb.Len() // 返回缓冲区的当前长度
}

// Reset 方法重置内部缓冲区，清空所有数据。
func (sb *SyncBuffer) Reset() {
	sb.Lock()         // 加锁，确保线程安全
	defer sb.Unlock() // 确保在函数返回时释放锁

	sb.bb.Reset() // 重置内部缓冲区
}

// Close 方法关闭缓冲区，清空所有数据，并通知所有等待的读写操作。
func (sb *SyncBuffer) Close() error {
	sb.Lock()         // 加锁，确保线程安全
	defer sb.Unlock() // 确保在函数返回时释放锁

	if sb.isClosed { // 如果缓冲区已关闭
		return nil // 返回 nil
	}

	sb.isClosed = true // 设置关闭标志

	sb.rwait.Signal() // 通知等待的读操作
	sb.wwait.Signal() // 通知等待的写操作

	sb.bb.Reset() // 重置内部缓冲区

	return nil
}

// NewSyncBuffer 函数用于创建一个新的 SyncBuffer 实例。
// 参数：
//   - maxLength：缓冲区的最大长度
//
// 返回值：
//   - *SyncBuffer：创建的 SyncBuffer 实例
func NewSyncBuffer(maxLength int) *SyncBuffer {
	sb := &SyncBuffer{
		bb:        bytes.NewBuffer(nil), // 创建一个新的字节缓冲区
		isClosed:  false,                // 初始化关闭标志为 false
		maxLength: maxLength,            // 设置缓冲区的最大长度
	}

	sb.rwait.L = &sb.Mutex // 设置读等待条件变量的锁
	sb.wwait.L = &sb.Mutex // 设置写等待条件变量的锁

	return sb
}
