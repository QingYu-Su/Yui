package handlers

import (
	"io"
	"os"
	"path"

	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// Download 创建一个SSH通道处理器，用于安全地从服务器下载文件到客户端
// 参数:
//   - dataDir: 服务器数据目录的根路径
//
// 返回值:
//   - ChannelHandler: 处理文件下载请求的函数
func Download(dataDir string) func(_ string, _ *users.User, newChannel ssh.NewChannel, log logger.Logger) {
	return func(_ string, _ *users.User, newChannel ssh.NewChannel, log logger.Logger) {
		// 1. 构建安全的下载路径
		// 首先将客户端请求的路径规范化为绝对路径（防止路径遍历攻击）
		downloadPath := path.Join("/", string(newChannel.ExtraData()))
		// 注意：必须分两步处理路径，直接使用path.Join("./downloads/", path)可能导致路径遍历漏洞
		// 将路径限定在指定的下载目录下（dataDir/downloads/...）
		downloadPath = path.Join(dataDir, "downloads", downloadPath)

		// 2. 验证请求的文件路径
		// 检查文件是否存在且不是目录
		stats, err := os.Stat(downloadPath)
		if err != nil && (os.IsNotExist(err) || !stats.IsDir()) {
			log.Warning("远程客户端请求了不存在的路径: '%s'", downloadPath)
			// 拒绝请求并返回错误信息
			newChannel.Reject(ssh.Prohibited, "file not found")
			return
		}

		log.Info("客户端正在下载文件: %s", downloadPath)

		// 3. 打开请求的文件
		f, err := os.Open(downloadPath)
		if err != nil {
			log.Warning("无法打开请求的文件路径: '%s': %s", downloadPath, err)
			// 拒绝请求并返回错误信息
			newChannel.Reject(ssh.Prohibited, "cannot open file")
			return
		}
		defer f.Close() // 确保函数退出时关闭文件

		// 4. 接受SSH通道连接
		c, r, err := newChannel.Accept()
		if err != nil {
			return // 如果接受通道失败则直接返回
		}
		defer c.Close() // 确保函数退出时关闭通道
		// 启动goroutine丢弃所有通道请求（因为我们只需要数据传输）
		go ssh.DiscardRequests(r)

		// 5. 将文件内容通过SSH通道传输到客户端
		_, err = io.Copy(c, f)
		if err != nil {
			log.Warning("向远程客户端传输文件失败: %s", err)
			return
		}
	}
}
