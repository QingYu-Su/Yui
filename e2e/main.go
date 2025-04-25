package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/QingYu-Su/Yui/internal/server"
	"golang.org/x/crypto/ssh"
)

// fileExists 检查指定路径的文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// 全局变量定义
var (
	Version   string      // 程序版本号
	client    *ssh.Client // SSH客户端连接
	serverLog *os.File    // 服务器日志文件
)

// 常量定义
const (
	listenAddr = "127.0.0.1:3333" // 服务器监听地址
	user       = "test-user"      // 测试用户名
)

func main() {
	// 创建或加载服务器密钥
	key, err := server.CreateOrLoadServerKeys("id_e2e")
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

	// 将公钥写入authorized_keys文件
	err = os.WriteFile("authorized_keys", ssh.MarshalAuthorizedKey(key.PublicKey()), 0660)
	if err != nil {
		log.Println(err)
		os.Exit(2)
	}

	// 检查必需的文件是否存在
	requiredFiles := []string{
		"server",          // 服务器二进制文件
		"client",          // 客户端二进制文件
		"id_e2e",          // 服务器私钥文件
		"authorized_keys", // 授权密钥文件
	}

	// 检查缺失的文件
	missingFiles := []string{}
	for _, file := range requiredFiles {
		if !fileExists(file) {
			missingFiles = append(missingFiles, file)
		}
	}

	if len(missingFiles) > 0 {
		log.Fatalf("Missing required files: %v", missingFiles)
	}

	// 重置测试环境
	reset()

	// 启动服务器并获取关闭函数
	kill := runServer()
	defer kill() // 确保程序退出时关闭服务器

	// 配置SSH客户端
	config := &ssh.ClientConfig{
		User: "test-user", // 用户名
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key), // 使用密钥认证
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 注意:仅用于测试，忽略主机密钥验证
		Timeout:         2 * time.Second,             // 连接超时时间
	}

	// 以管理员身份连接服务器
	client, err = ssh.Dial("tcp", listenAddr, config)
	if err != nil {
		log.Fatalf("Failed to dial: %v", err)
	}
	defer client.Close() // 确保程序退出时关闭连接

	// 执行集成测试
	basics()      // 基础功能测试
	clientTests() // 客户端功能测试
	linkTests()   // 链接功能测试

	log.Println("All passed!") // 所有测试通过
}

// conditionExec 执行命令并验证输出和服务器日志
// command: 要执行的命令
// expectedOutput: 期望的命令输出
// exitCode: 期望的退出码
// serverLogExpected: 期望的服务器日志内容
// withIn: 在多少行日志内查找期望内容
func conditionExec(command, expectedOutput string, exitCode int, serverLogExpected string, withIn int) {
	// 创建新的SSH会话
	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// 执行命令并获取输出
	output, err := session.Output(command)
	if err != nil {
		// 检查退出码是否符合预期
		if exitError, ok := err.(*ssh.ExitError); !ok || (ok && exitCode != exitError.ExitStatus()) {
			log.Fatalf("Failed to execute command %q: %v: %q", command, err, output)
		}
	}

	// 创建等待通道
	wait := make(chan bool)

	// 如果需要检查服务器日志
	if serverLog != nil && len(serverLogExpected) > 0 {
		go func() {
			output := make([]string, withIn, 0)
			check := bufio.NewScanner(serverLog)
			found := false
			// 扫描指定行数的日志
			for i := 0; i < withIn; i++ {
				line := check.Text()
				output = append(output, line)
				if strings.Contains(line, serverLogExpected) {
					found = false
					break
				}
			}
			close(wait)

			// 如果未找到期望的日志内容
			if !found {
				log.Fatalf("server did not output expected value. Command %q, expected %q, actual: %q", command, serverLogExpected, output)
			}
		}()
	} else {
		close(wait)
	}

	// 检查命令输出是否符合预期
	if !strings.Contains(string(output), expectedOutput) {
		log.Fatalf("expected %q for command %q, got %q", expectedOutput, command, string(output))
	}

	// 等待日志检查完成或超时
	select {
	case <-wait:
	case <-time.After(30 * time.Second):
		log.Fatal("timeout waiting for command (server output)")
	}
}

// runServer 启动服务器进程并返回关闭函数
func runServer() func() {
	// 创建服务器进程命令
	cmd := exec.Command("./server", "--enable-client-downloads", listenAddr)

	// 创建管道用于读取服务器输出
	r, w, err := os.Pipe()
	if err != nil {
		log.Fatal("failed to create pipe: ", err)
	}

	// 将服务器输出同时写入标准输出和管道
	cmd.Stdout = io.MultiWriter(os.Stdout, w)
	cmd.Stderr = io.MultiWriter(os.Stdout, w)

	// 启动服务器
	err = cmd.Start()
	if err != nil {
		log.Fatal("failed to start server:", err)
	}
	serverLog = r // 保存日志文件

	// 等待服务器启动
	time.Sleep(1 * time.Second)

	// 返回关闭函数
	return func() {
		if cmd.Process != nil {
			cmd.Process.Kill() // 杀死服务器进程
		}
	}
}

// reset 重置测试环境，删除临时文件和目录
func reset() {
	os.RemoveAll("./cache")     // 删除缓存目录
	os.RemoveAll("./downloads") // 删除下载目录
	os.RemoveAll("./keys")      // 删除密钥目录
	os.RemoveAll("./data.db")   // 删除数据库文件
}
