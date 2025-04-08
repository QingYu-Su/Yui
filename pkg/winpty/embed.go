//go:build windows
// +build windows
// 构建约束，确保这段代码仅在 Windows 系统上编译和运行。

package winpty
// 定义了一个名为 winpty 的包，用于封装与 Windows Pseudo-TTY（伪终端）相关的功能。

import (
	"embed" // 提供嵌入文件系统的功能。
	"errors" // 用于处理错误。
	"fmt"    // 提供格式化输入输出的功能。
	"log"    // 提供日志记录功能。
	"os"     // 提供操作系统相关功能。
	"path"   // 提供路径操作功能。
	"runtime" // 提供运行时信息，如操作系统和架构。
	"syscall" // 提供对系统调用的访问。
	"unsafe"  // 提供对底层内存操作的功能。

	"golang.org/x/sys/windows"
	// 提供对 Windows 系统调用的扩展支持。
)

//go:embed executables/*
var binaries embed.FS
// 嵌入包含 winpty 相关可执行文件的文件系统。

func writeBinaries(dllPath, agentPage string) error {
	// 将 winpty.dll 和 winpty-agent.exe 写入到指定路径。

	vsn := windows.RtlGetVersion()
	// 获取当前 Windows 操作系统的版本信息。

	/*
		<url id="cvqkjeqmisdi8mbk1o5g" type="url" status="parsed" title="操作系统版本 - Win32 apps" wc="3773">https://msdn.microsoft.com/en-us/library/ms724832(VS.85).aspx</url> 
		Windows 10					10.0*
		Windows Server 2016			10.0*
		Windows 8.1					6.3*
		Windows Server 2012 R2		6.3*
		Windows 8					6.2
		Windows Server 2012			6.2
		Windows 7					6.1
		Windows Server 2008 R2		6.1
		Windows Server 2008			6.0
		Windows Vista				6.0
		Windows Server 2003 R2		5.2
		Windows Server 2003			5.2
		Windows XP 64-Bit Edition	5.2
		Windows XP					5.1
		Windows 2000				5.0
	*/
	// 上述注释引用了 Windows 操作系统版本号的官方文档。

	dllType := "regular"
	// 默认使用普通版本的 DLL。

	if vsn.MajorVersion == 5 {
		// 如果操作系统版本是 Windows XP 或 Windows 2000。
		if runtime.GOARCH == "arm64" {
			// 如果是 arm64 架构，Windows XP 没有 arm64 版本。
			log.Println("xp doesnt have an arm64 version so uh, Im just going to die here")
			return errors.New("tried to run an arm64 windows xp winpty session")
		}
		dllType = "xp"
		// 使用针对 Windows XP 的特殊版本的 DLL。
	}

	dll, err := binaries.ReadFile(path.Join("executables", runtime.GOARCH, dllType, "winpty.dll"))
	// 从嵌入的文件系统中读取 winpty.dll 文件。
	if err != nil {
		panic(err)
		// 如果读取失败，直接 panic。
	}

	err = os.WriteFile(dllPath, dll, 0700)
	// 将 winpty.dll 写入到指定路径。
	if err != nil {
		return err
		// 如果写入失败，返回错误。
	}

	exe, err := binaries.ReadFile(path.Join("executables", runtime.GOARCH, dllType, "winpty-agent.exe"))
	// 从嵌入的文件系统中读取 winpty-agent.exe 文件。
	if err != nil {
		panic(err)
		// 如果读取失败，直接 panic。
	}

	err = os.WriteFile(agentPage, exe, 0700)
	// 将 winpty-agent.exe 写入到指定路径。
	if err != nil {
		return err
		// 如果写入失败，返回错误。
	}

	return nil
	// 如果一切成功，返回 nil。
}

func createAgentCfg(flags uint32) (uintptr, error) {
	// 创建一个新的代理配置。

	var errorPtr uintptr
	// 初始化错误指针。

	if winpty_error_free == nil {
		return uintptr(0), errors.New("winpty was not initalised")
		// 如果 winpty_error_free 未初始化，返回错误。
	}

	err := winpty_error_free.Find() // 检查 DLL 是否可用
	if err != nil {
		return uintptr(0), err
		// 如果 DLL 不可用，返回错误。
	}

	defer winpty_error_free.Call(errorPtr)
	// 确保在函数退出时释放错误对象。

	agentCfg, _, _ := winpty_config_new.Call(uintptr(flags), uintptr(unsafe.Pointer(errorPtr)))
	// 调用 winpty_config_new 创建代理配置。
	if agentCfg == uintptr(0) {
		return 0, fmt.Errorf("Unable to create agent config, %s", GetErrorMessage(errorPtr))
		// 如果创建失败，返回错误。
	}

	return agentCfg, nil
	// 返回创建的代理配置指针。
}

func createSpawnCfg(flags uint32, appname, cmdline, cwd string, env []string) (uintptr, error) {
	// 创建一个新的启动配置。

	var errorPtr uintptr
	defer winpty_error_free.Call(errorPtr)
	// 初始化错误指针并确保在函数退出时释放错误对象。

	cmdLineStr, err := syscall.UTF16PtrFromString(cmdline)
	// 将命令行字符串转换为 UTF-16 指针。
	if err != nil {
		return 0, fmt.Errorf("Failed to convert cmd to pointer.")
		// 如果转换失败，返回错误。
	}

	appNameStr, err := syscall.UTF16PtrFromString(appname)
	// 将应用程序名称字符串转换为 UTF-16 指针。
	if err != nil {
		return 0, fmt.Errorf("Failed to convert app name to pointer.")
		// 如果转换失败，返回错误。
	}

	cwdStr, err := syscall.UTF16PtrFromString(cwd)
	// 将工作目录字符串转换为 UTF-16 指针。
	if err != nil {
		return 0, fmt.Errorf("Failed to convert working directory to pointer.")
		// 如果转换失败，返回错误。
	}

	envStr, err := UTF16PtrFromStringArray(env)
	// 将环境变量数组转换为 UTF-16 指针。
	if err != nil {
		return 0, fmt.Errorf("Failed to convert cmd to pointer.")
		// 如果转换失败，返回错误。
	}

	var spawnCfg uintptr
	if runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64" {
		// 如果是 64 位架构。
		spawnCfg, _, _ = winpty_spawn_config_new.Call(
			uintptr(flags),
			uintptr(unsafe.Pointer(appNameStr)),
			uintptr(unsafe.Pointer(cmdLineStr)),
			uintptr(unsafe.Pointer(cwdStr)),
			uintptr(unsafe.Pointer(envStr)),
			uintptr(unsafe.Pointer(errorPtr)),
		)
	} else {
		// 如果是 32 位架构。
		spawnCfg, _, _ = winpty_spawn_config_new.Call(
			uintptr(flags),
			uintptr(0), // winpty 需要一个 UINT64，因此需要在 32 位架构上填充。
			uintptr(unsafe.Pointer(appNameStr)),
			uintptr(unsafe.Pointer(cmdLineStr)),
			uintptr(unsafe.Pointer(cwdStr)),
			uintptr(unsafe.Pointer(envStr)),
			uintptr(unsafe.Pointer(errorPtr)),
		)
	}

	if spawnCfg == uintptr(0) {
		return 0, fmt.Errorf("Unable to create spawn config, %s", GetErrorMessage(errorPtr))
		// 如果创建失败，返回错误。
	}

	return spawnCfg, nil
	// 返回创建的启动配置指针。
}