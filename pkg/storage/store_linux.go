package storage

import (
	"fmt" // 导入用于格式化输出的包
	"io"  // 导入用于处理输入输出的包
	"os"  // 导入用于操作文件系统的包

	"golang.org/x/sys/unix" // 导入用于调用 Linux 系统调用的包
)

// Store 函数用于将数据存储到一个匿名文件中（仅限 Linux 系统）。
// 参数：
//   - filename：目标文件的名称（仅用于日志或错误处理，实际存储不会使用该文件名）
//   - r：io.ReadCloser 类型的读取器，用于读取要存储的数据
//
// 返回值：
//   - string：成功存储后的文件路径（匿名文件的路径）
//   - error：如果发生错误，返回错误信息
func Store(filename string, r io.ReadCloser) (string, error) {
	// 使用 unix.MemfdCreate 创建一个匿名文件
	// 参数：
	//   - ""：匿名文件的名称（这里为空字符串，表示不需要特定名称）
	//   - unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING：标志位，表示文件描述符在执行 exec 时关闭，并允许对文件进行密封
	fd, err := unix.MemfdCreate("", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		// 如果创建匿名文件失败，调用 StoreDisk 函数将数据存储到磁盘文件中
		return StoreDisk(filename, r)
	}

	// 使用 os.NewFile 将文件描述符包装为一个 *os.File 对象
	mfd := os.NewFile(uintptr(fd), "")

	// 将读取器 r 中的数据复制到匿名文件中
	_, err = io.Copy(mfd, r)
	if err != nil {
		// 如果复制数据失败，调用 StoreDisk 函数将数据存储到磁盘文件中
		return StoreDisk(filename, r)
	}

	// 返回匿名文件的路径
	// 在 Linux 系统中，匿名文件可以通过 /proc/self/fd/<fd> 访问
	return fmt.Sprintf("/proc/self/fd/%d", fd), nil
}
