package tcp

import (
	"io"      // 用于处理 I/O 操作
	"log"     // 用于记录日志
	"net"     // 用于网络相关操作
	"os"      // 用于操作系统相关功能
	"strings" // 用于处理字符串
	"time"    // 用于处理时间相关操作

	"github.com/QingYu-Su/Yui/internal/server/data" // 导入数据模块，用于操作数据库
	"github.com/QingYu-Su/Yui/pkg/logger"           // 导入日志模块，用于记录日志
)

// handleBashConn 处理一个基于 TCP 的 Bash 原始连接
func handleBashConn(conn net.Conn) {
	defer conn.Close() // 确保连接在函数退出时关闭

	// 创建一个日志记录器，记录与该连接相关的日志
	downloadLog := logger.NewLog(conn.RemoteAddr().String())

	// 设置连接的读取截止时间，防止连接阻塞
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	// 用于存储文件 ID 的缓冲区，包括 64 字节的文件 ID 和 3 字节的 RAW 头部前缀
	fileID := make([]byte, 67)

	// 从连接中读取文件 ID
	n, err := conn.Read(fileID)
	if err != nil {
		// 如果读取失败，记录警告日志并退出
		downloadLog.Warning("failed to download file using raw tcp: %s", err)
		return
	}

	// 取消连接的截止时间限制
	conn.SetDeadline(time.Time{})

	// 检查读取的字节数是否有效
	if n == 0 || n < 3 {
		// 如果读取的字节数无效，记录警告日志并退出
		downloadLog.Warning("received malformed raw download request")
		return
	}

	// 提取文件名（从第 3 个字节开始到读取的末尾）
	filename := strings.TrimSpace(string(fileID[3:n]))

	// 从数据库中获取下载文件的信息
	f, err := data.GetDownload(filename)
	if err != nil {
		// 如果获取失败，记录警告日志并退出
		downloadLog.Warning("failed to get file %q: err %s", filename, err)
		return
	}

	// 打开文件以供下载
	file, err := os.Open(f.FilePath)
	if err != nil {
		// 如果打开文件失败，记录警告日志并退出
		downloadLog.Warning("failed to open file %q for download: %s", f.FilePath, err)
		return
	}
	defer file.Close() // 确保文件在函数退出时关闭

	// 记录成功下载的日志
	downloadLog.Info("downloaded %q using RAW tcp method", filename)

	// 将文件内容复制到连接中，完成文件传输
	io.Copy(conn, file)
}

// Start 启动一个基于 TCP 的原始下载服务器
func Start(listener net.Listener) {
	// 记录服务器启动的日志
	log.Println("Started Raw Download Server")

	// 无限循环，接受客户端连接
	for {
		conn, err := listener.Accept()
		if err != nil {
			// 如果接受连接失败，记录错误日志并退出
			log.Printf("failed to accept raw download connection: %s", err)
			return
		}

		// 启动一个 goroutine 处理每个连接
		go handleBashConn(conn)
	}
}
