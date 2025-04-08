//go:build windows
// +build windows
// 这两行是 Go 的构建约束，表示这段代码仅在 Windows 系统上编译和运行。

package winpty
// 定义了一个名为 winpty 的包，用于封装与 Windows Pseudo-TTY（伪终端）相关的功能。

import (
	"errors" // 用于处理错误
	"log"    // 用于日志记录
	"os"     // 提供操作系统相关功能
	"path"   // 提供路径操作功能
	"runtime" // 提供运行时信息，如操作系统和架构
	"syscall" // 提供对系统调用的访问
)

const (
	WINPTY_SPAWN_FLAG_AUTO_SHUTDOWN            = 1
	// 定义了一个常量，表示在启动进程时，当最后一个客户端断开连接时自动关闭代理。
	WINPTY_FLAG_ALLOW_CURPROC_DESKTOP_CREATION = 0x8
	// 定义了一个常量，表示允许当前进程创建桌面。
)

var (
	modWinPTY *syscall.LazyDLL
	// 用于存储加载的 winpty.dll 模块。

	// Error handling...
	winpty_error_code *syscall.LazyProc
	// 获取错误代码的函数。
	winpty_error_msg  *syscall.LazyProc
	// 获取错误消息的函数。
	winpty_error_free *syscall.LazyProc
	// 释放错误对象的函数。

	// Configuration of a new agent.
	winpty_config_new               *syscall.LazyProc
	// 创建一个新的代理配置的函数。
	winpty_config_free              *syscall.LazyProc
	// 释放代理配置的函数。
	winpty_config_set_initial_size  *syscall.LazyProc
	// 设置代理的初始大小的函数。
	winpty_config_set_mouse_mode    *syscall.LazyProc
	// 设置代理的鼠标模式的函数。
	winpty_config_set_agent_timeout *syscall.LazyProc
	// 设置代理的超时时间的函数。

	// Start the agent.
	winpty_open          *syscall.LazyProc
	// 打开一个代理的函数。
	winpty_agent_process *syscall.LazyProc
	// 获取代理进程的函数。

	// I/O Pipes
	winpty_conin_name  *syscall.LazyProc
	// 获取代理的输入管道名称的函数。
	winpty_conout_name *syscall.LazyProc
	// 获取代理的输出管道名称的函数。
	winpty_conerr_name *syscall.LazyProc
	// 获取代理的错误管道名称的函数。

	// Agent RPC Calls
	winpty_spawn_config_new  *syscall.LazyProc
	// 创建一个新的启动配置的函数。
	winpty_spawn_config_free *syscall.LazyProc
	// 释放启动配置的函数。
	winpty_spawn             *syscall.LazyProc
	// 启动进程的函数。
	winpty_set_size          *syscall.LazyProc
	// 设置代理大小的函数。
	winpty_free              *syscall.LazyProc
	// 释放代理的函数。
)

func loadWinPty() error {
	// 加载 winpty.dll 模块并初始化相关函数。

	if modWinPTY != nil {
		// 如果已经加载过 winpty.dll，则直接返回。
		return nil
	}

	switch runtime.GOARCH {
	case "amd64", "arm64", "386":
		// 支持的架构类型。
	default:
		// 如果当前架构不支持，则返回错误。
		return errors.New("unsupported winpty platform " + runtime.GOARCH)
	}

	var (
		winptyDllName   = "winpty.dll"
		// winpty.dll 的默认名称。
		winptyAgentName = "winpty-agent.exe"
		// winpty-agent.exe 的默认名称。
	)

	cacheDir, err := os.UserCacheDir()
	// 获取用户的缓存目录。
	if err != nil {
		// 如果获取失败，则记录日志。
		log.Println("unable to get cache directory for writing winpty pe's writing may fail if directory is read only")
	}

	if err == nil {
		// 如果获取成功，则将 winpty.dll 和 winpty-agent.exe 的路径设置为缓存目录下的路径。
		winptyDllName = path.Join(cacheDir, winptyDllName)
		winptyAgentName = path.Join(cacheDir, winptyAgentName)
	}

	err = writeBinaries(winptyDllName, winptyAgentName)
	// 将 winpty.dll 和 winpty-agent.exe 写入到指定路径。
	if err != nil {
		// 如果写入失败，则返回错误。
		return errors.New("writing PEs to disk failed: " + err.Error())
	}

	modWinPTY = syscall.NewLazyDLL(winptyDllName)
	// 加载 winpty.dll 模块。
	if modWinPTY == nil {
		// 如果加载失败，则返回错误。
		return errors.New("creating lazy dll failed")
	}

	// 初始化错误处理相关的函数。
	winpty_error_code = modWinPTY.NewProc("winpty_error_code")
	winpty_error_msg = modWinPTY.NewProc("winpty_error_msg")
	winpty_error_free = modWinPTY.NewProc("winpty_error_free")

	// 初始化代理配置相关的函数。
	winpty_config_new = modWinPTY.NewProc("winpty_config_new")
	winpty_config_free = modWinPTY.NewProc("winpty_config_free")
	winpty_config_set_initial_size = modWinPTY.NewProc("winpty_config_set_initial_size")
	winpty_config_set_mouse_mode = modWinPTY.NewProc("winpty_config_set_mouse_mode")
	winpty_config_set_agent_timeout = modWinPTY.NewProc("winpty_config_set_agent_timeout")

	// 初始化启动代理相关的函数。
	winpty_open = modWinPTY.NewProc("winpty_open")
	winpty_agent_process = modWinPTY.NewProc("winpty_agent_process")

	// 初始化 I/O 管道相关的函数。
	winpty_conin_name = modWinPTY.NewProc("winpty_conin_name")
	winpty_conout_name = modWinPTY.NewProc("winpty_conout_name")
	winpty_conerr_name = modWinPTY.NewProc("winpty_conerr_name")

	// 初始化代理 RPC 调用相关的函数。
	winpty_spawn_config_new = modWinPTY.NewProc("winpty_spawn_config_new")
	winpty_spawn_config_free = modWinPTY.NewProc("winpty_spawn_config_free")
	winpty_spawn = modWinPTY.NewProc("winpty_spawn")
	winpty_set_size = modWinPTY.NewProc("winpty_set_size")
	winpty_free = modWinPTY.NewProc("winpty_free")

	return nil
	// 如果加载成功，则返回 nil。
}