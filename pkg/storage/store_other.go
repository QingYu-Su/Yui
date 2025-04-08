//go:build !linux

// 上述指令表示这段代码仅在非 Linux 系统上编译和运行。
// 如果在 Linux 系统上编译，这段代码将被忽略。

package storage

import (
	"io" // 导入用于处理输入输出的包
)

// Store 函数是一个简单的包装函数，用于将数据存储到磁盘文件中。
// 它的作用是将调用者对存储功能的请求转发到 StoreDisk 函数。
// 参数：
//   - filename：目标文件的路径
//   - r：io.ReadCloser 类型的读取器，用于读取要存储的数据
//
// 返回值：
//   - string：成功存储后的文件路径
//   - error：如果发生错误，返回错误信息
func Store(filename string, r io.ReadCloser) (string, error) {
	// 直接调用 StoreDisk 函数，将参数传递给它，并返回其结果。
	// StoreDisk 函数负责实际的文件存储逻辑。
	return StoreDisk(filename, r)
}
