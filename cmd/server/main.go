package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server"
	"github.com/QingYu-Su/Yui/internal/terminal"
	"github.com/QingYu-Su/Yui/pkg/logger"
)

// printHelp 打印程序使用帮助信息
// 显示程序的命令行参数选项和使用方法
func printHelp() {
	// 打印基本用法
	fmt.Println("usage: ", filepath.Base(os.Args[0]), "[options] listen_address")
	fmt.Println("\nOptions:")

	// 数据相关选项
	fmt.Println("  Data")
	fmt.Println("\t--datadir\t\tDirectory to search for keys, config files, and to store compile cache (defaults to working directory)")

	// 授权相关选项
	fmt.Println("  Authorisation")
	fmt.Println("\t--insecure\t\tIgnore authorized_controllee_keys file and allow any RSSH client to connect")
	fmt.Println("\t--openproxy\t\tAllow any ssh client to do a dynamic remote forward (-R) and effectively allowing anyone to open a port on localhost on the server")

	// 网络相关选项
	fmt.Println("  Network")
	fmt.Println("\t--tls\t\t\tEnable TLS on socket (ssh/http over TLS)")
	fmt.Println("\t--tlscert\t\tTLS certificate path")
	fmt.Println("\t--tlskey\t\tTLS key path")
	fmt.Println("\t--webserver\t\t(Depreciated) Enable webserver on the listen_address port")
	fmt.Println("\t--enable-client-downloads\t\tEnable webserver and raw TCP to download clients")
	fmt.Println("\t--external_address\tIf the external IP and port of the RSSH server is different from the listening address, set that here")
	fmt.Println("\t--timeout\t\tSet rssh client timeout (when a client is considered disconnected) defaults, in seconds, defaults to 5, if set to 0 timeout is disabled")

	// 实用工具选项
	fmt.Println("  Utility")
	fmt.Println("\t--fingerprint\t\tPrint fingerprint and exit. (Will generate server key if none exists)")
	fmt.Println("\t--log-level\t\tChange logging output levels (will set default log level for generated clients), [INFO,WARNING,ERROR,FATAL,DISABLED]")
	fmt.Println("\t--console-label\t\tChange console label.  (Default: catcher)")
}

// main 函数是程序的入口点
func main() {
	// 解析命令行参数，定义有效的标志选项
	options, err := terminal.ParseLineValidFlags(strings.Join(os.Args, " "), 0, map[string]bool{
		"insecure":                true, // 不安全模式标志
		"tls":                     true, // 启用TLS标志
		"tlscert":                 true, // TLS证书路径标志
		"tlskey":                  true, // TLS密钥路径标志
		"external_address":        true, // 外部地址标志
		"fingerprint":             true, // 显示指纹标志
		"webserver":               true, // 启用Web服务器标志(已弃用)
		"enable-client-downloads": true, // 启用客户端下载标志
		"datadir":                 true, // 数据目录路径标志
		"h":                       true, // 帮助短标志
		"help":                    true, // 帮助长标志
		"timeout":                 true, // 超时设置标志
		"openproxy":               true, // 开放代理标志
		"log-level":               true, // 日志级别标志
		"console-label":           true, // 控制台标签标志
	})

	if err != nil {
		fmt.Println(err)
		printHelp() // 显示帮助信息
		return
	}

	// 检查是否请求帮助
	if options.IsSet("h") || options.IsSet("help") {
		printHelp()
		return
	}

	// 获取数据目录路径，默认为当前目录
	dataDir, err := options.GetArgString("datadir")
	if err != nil {
		dataDir = "."
	}

	// 获取数据目录的绝对路径
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		log.Fatalf("无法解析提供的数据目录路径: %v", err)
	}

	// 检查数据目录是否存在并有正确权限
	dataDirStat, err := os.Stat(dataDir)
	if err != nil {
		log.Fatalf("无法访问数据目录 %s - 目录是否存在并有正确权限?", dataDir)
	}

	// 确保数据目录是目录而不是文件
	if !dataDirStat.IsDir() {
		log.Fatalf("指定的数据目录 %s 不是目录", dataDir)
	}

	log.Printf("从 %s 加载文件\n", dataDir)

	// 设置日志级别
	var logLevel string
	var ok bool

	logLevel, err = options.GetArgString("log-level")
	ok = err == nil
	if err != nil {
		// 尝试从环境变量获取日志级别
		logLevel, ok = os.LookupEnv("RSSH_LOG_LEVEL")
	}

	if ok {
		// 转换并设置日志级别
		urg, err := logger.StrToUrgency(logLevel)
		if err != nil {
			log.Fatal(err)
		}
		logger.SetLogLevel(urg)
	}

	// 处理指纹显示请求
	if options.IsSet("fingerprint") {
		private, err := server.CreateOrLoadServerKeys(filepath.Join(dataDir, "id_ed25519"))
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println(internal.FingerprintSHA256Hex(private.PublicKey()))
		return
	}

	// 检查是否提供了监听地址
	if len(options.Arguments) < 1 {
		fmt.Println("缺少监听地址")
		printHelp()
		return
	}

	// 获取监听地址
	listenAddress := options.Arguments[len(options.Arguments)-1].Value()

	// 设置超时时间，默认为5秒
	var timeout int = 5
	if timeoutString, err := options.GetArgString("timeout"); err == nil {
		timeout, err = strconv.Atoi(timeoutString)
		if err != nil {
			fmt.Printf("无法将 '%s' 转换为整数\n", timeoutString)
			printHelp()
			return
		}

		if timeout < 0 {
			fmt.Printf("超时时间不能小于0\n")
			printHelp()
			return
		}

		if timeout == 0 {
			log.Println("超时/保持连接已禁用，如果客户端断开连接可能会导致问题")
		}
	}

	// 获取安全相关设置
	insecure := options.IsSet("insecure")   // 不安全模式
	openproxy := options.IsSet("openproxy") // 开放代理模式

	// 设置控制台标签
	potentialConsoleLabel, err := options.GetArgString("console-label")
	if err == nil {
		internal.ConsoleLabel = strings.TrimSpace(potentialConsoleLabel)
	} else {
		// 尝试从环境变量获取控制台标签
		potentialConsoleLabel, ok := os.LookupEnv("RSSH_CONSOLE_LABEL")
		if ok {
			internal.ConsoleLabel = strings.TrimSpace(potentialConsoleLabel)
		}
	}

	// 获取TLS相关设置
	tls := options.IsSet("tls")                   // 是否启用TLS
	tlscert, _ := options.GetArgString("tlscert") // TLS证书路径
	tlskey, _ := options.GetArgString("tlskey")   // TLS密钥路径

	// 确定是否启用下载功能
	enabledDownloads := options.IsSet("webserver") || options.IsSet("enable-client-downloads")

	// 处理已弃用的webserver标志
	if options.IsSet("webserver") {
		log.Println("[警告] --webserver 已弃用，请使用 --enable-client-downloads")
	}

	// 获取连接回传地址
	connectBackAddress, err := options.GetArgString("external_address")

	autogeneratedConnectBack := false
	// 如果未指定外部地址但启用了下载功能，则自动生成连接回传地址
	if err != nil && enabledDownloads {
		autogeneratedConnectBack = true
		connectBackAddress = listenAddress

		// 特殊处理监听所有接口的情况(如:3232)
		addressParts := strings.Split(listenAddress, ":")
		if len(addressParts) > 0 && len(addressParts[0]) == 0 {
			port := addressParts[1]

			// 获取网络接口信息
			ifaces, err := net.Interfaces()
			if err == nil {
				for _, i := range ifaces {
					addrs, err := i.Addrs()
					if err != nil {
						continue
					}

					if len(addrs) == 0 {
						continue
					}

					// 跳过回环接口
					if i.Flags&net.FlagLoopback == 0 {
						connectBackAddress = strings.Split(addrs[0].String(), "/")[0] + ":" + port
						break
					}
				}
			}
		}
	}

	log.Println("连接回传地址: ", connectBackAddress)

	// 启动服务器
	server.Run(listenAddress, dataDir, connectBackAddress, autogeneratedConnectBack, tlscert, tlskey, insecure, enabledDownloads, tls, openproxy, timeout)
}
