package webserver

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server/data"
	"github.com/QingYu-Su/Yui/internal/server/webserver/shellscripts"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"golang.org/x/crypto/ssh"
)

var (
	// DefaultConnectBack 存储服务端默认的连接地址，用于客户端连接
	DefaultConnectBack string

	// defaultFingerPrint 存储默认的指纹，用于验证
	defaultFingerPrint string

	// projectRoot 存储项目的根目录路径
	projectRoot string

	// webserverOn 标志，表示 Web 服务器是否已启动
	webserverOn bool
)

// Start 初始化并启动 Web 服务器
func Start(webListener net.Listener, connectBackAddress string, autogeneratedConnectBack bool, projRoot, dataDir string, publicKey ssh.PublicKey) {
	// 设置项目根目录
	projectRoot = projRoot

	// 设置默认回调地址
	DefaultConnectBack = connectBackAddress

	// 生成默认指纹
	defaultFingerPrint = internal.FingerprintSHA256Hex(publicKey)

	// 初始化构建管理器，设置缓存路径
	err := startBuildManager(filepath.Join(dataDir, "cache"))
	if err != nil {
		log.Fatal(err) // 如果初始化失败，记录错误并退出
	}

	// 创建 HTTP 服务器
	srv := &http.Server{
		ReadTimeout:  60 * time.Second,                        // 设置读取超时时间为 60 秒
		WriteTimeout: 60 * time.Second,                        // 设置写入超时时间为 60 秒
		Handler:      buildAndServe(autogeneratedConnectBack), // 设置请求处理器
	}

	// 记录日志，表示 Web 服务器已启动
	log.Println("Started Web Server")
	webserverOn = true // 设置 Web 服务器启动标志

	// 启动 Web 服务器，监听传入的连接
	log.Fatal(srv.Serve(webListener))
}

// 定义一个 404 Not Found 的 HTML 页面
const notFound = `<html>
<head><title>404 Not Found</title></head>
<body>
<center><h1>404 Not Found</h1></center>
<hr><center>nginx</center>
</body>
</html>`

// buildAndServe 是一个 HTTP 请求处理函数，用于处理客户端的请求
func buildAndServe(autogeneratedConnectBack bool) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {

		// 创建一个日志记录器，记录请求的来源和主机信息
		httpDownloadLog := logger.NewLog(fmt.Sprintf("%s:%q", req.RemoteAddr, req.Host))

		// 记录请求路径
		httpDownloadLog.Info("Web Server got hit:  %q", req.URL.Path)

		// 从请求路径中提取文件名
		filename := strings.TrimPrefix(req.URL.Path, "/")
		linkExtension := filepath.Ext(filename) // 获取文件扩展名

		// 去掉扩展名后的文件名
		filenameWithoutExtension := strings.TrimSuffix(filename, linkExtension)

		// 尝试从数据存储中获取下载信息
		f, err := data.GetDownload(filename)
		if err != nil {
			// 如果获取失败，尝试去掉扩展名后再次获取
			f, err = data.GetDownload(filenameWithoutExtension)
			if err != nil {
				// 如果仍然失败，记录错误并返回 404 页面
				log.Println("could not get: ", filenameWithoutExtension, " err: ", err)

				w.Header().Set("content-type", "text/html")
				w.Header().Set("server", "nginx")
				w.Header().Set("Connection", "keep-alive")

				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(notFound))
				return
			}
		}

		// 如果请求的文件有扩展名，则使用模板生成对应的文件下载脚本，并返回给客户端
		if linkExtension != "" {

			// 确定回调地址
			host := DefaultConnectBack
			if autogeneratedConnectBack || f.UseHostHeader {
				host = req.Host
			}

			// 分割主机和端口
			host, port, err := net.SplitHostPort(host)
			if err != nil {
				// 如果分割失败，使用默认值
				host = DefaultConnectBack
				port = "80"

				httpDownloadLog.Info("no port specified in external_address: %s defaulting to: %s", DefaultConnectBack, DefaultConnectBack+":80")
			}

			// 生成动态内容
			output, err := shellscripts.MakeTemplate(shellscripts.Args{
				OS:               f.Goos,
				Arch:             f.Goarch,
				Name:             filenameWithoutExtension,
				Host:             host,
				Port:             port,
				Protocol:         "http",
				WorkingDirectory: f.WorkingDirectory,
			}, linkExtension[1:])
			if err != nil {
				// 如果生成失败，返回 404 页面
				w.Header().Set("content-type", "text/html")
				w.Header().Set("server", "nginx")
				w.Header().Set("Connection", "keep-alive")

				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(notFound))
				return
			}

			// 设置响应头并返回生成的内容
			w.Header().Set("Content-Disposition", "attachment; filename="+filename)
			w.Header().Set("Content-Type", "application/octet-stream")

			w.Write(output)
			return
		}

		// 如果请求的是一个文件
		file, err := os.Open(f.FilePath)
		if err != nil {
			// 如果文件打开失败，记录错误并返回 500 错误
			httpDownloadLog.Error("failed to open file for http download: %s", err)
			http.Error(w, "Error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// 根据文件类型确定扩展名
		var extension string

		switch f.FileType {
		case "shared-object":
			if f.Goos != "windows" {
				extension = ".so"
			} else if f.Goos == "windows" {
				extension = ".dll"
			}
		case "executable":
			if f.Goos == "windows" {
				extension = ".exe"
			}
		default:

		}

		// 设置响应头并返回文件内容
		w.Header().Set("Content-Disposition", "attachment; filename="+strings.TrimSuffix(filename, extension)+extension)
		w.Header().Set("Content-Type", "application/octet-stream")

		io.Copy(w, file)
	}
}
