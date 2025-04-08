package storage

import (
	"io"  // 导入用于处理输入输出的包
	"os"  // 导入用于操作文件系统的包
)

// StoreDisk 函数用于将数据存储到磁盘文件中
// 参数：
//   - path：目标文件的路径
//   - r：io.ReadCloser 类型的读取器，用于读取要存储的数据
// 返回值：
//   - path：成功存储后的文件路径
//   - error：如果发生错误，返回错误信息
func StoreDisk(path string, r io.ReadCloser) (string, error) {
	// 使用 os.Create 创建目标文件
	// 如果文件已存在，会截断文件内容；如果文件不存在，会创建新文件
	out, err := os.Create(path)
	if err != nil {
		// 如果创建文件失败，返回错误
		return "", err
	}
	defer out.Close() // 确保在函数返回时关闭文件

	// 设置文件权限为 0700（仅允许文件所有者读写执行）
	err = os.Chmod(path, 0700)
	if err != nil {
		// 如果设置文件权限失败，返回错误
		return "", err
	}

	// 使用 io.Copy 将读取器 r 中的数据复制到文件 out 中
	// io.Copy 会读取 r 中的所有数据并写入到 out，直到读取到 EOF 或发生错误
	_, err = io.Copy(out, r)
	if err != nil {
		// 如果复制数据失败，返回错误
		return "", err
	}

	// 如果一切正常，返回存储后的文件路径和 nil 作为错误值
	return path, err
}