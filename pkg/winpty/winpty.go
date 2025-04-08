//go:build windows
// +build windows

// 构建约束，确保这段代码仅在 Windows 系统上编译和运行。

package winpty

// 定义了一个名为 winpty 的包，用于封装与 Windows Pseudo-TTY（伪终端）相关的功能。

import (
	"fmt"     // 提供格式化输入输出的功能。
	"os"      // 提供操作系统相关功能。
	"syscall" // 提供对系统调用的访问。
	"unsafe"  // 提供对底层内存操作的功能。

	"golang.org/x/sys/windows"
	// 提供对 Windows 系统调用的扩展支持。
)

// Options 定义了创建 WinPTY 时的配置选项。
type Options struct {
	// AppName 设置控制台的标题。
	AppName string

	// Command 是要启动的完整命令。
	Command string

	// Dir 设置命令的工作目录。
	Dir string

	// Env 设置环境变量，格式为 VAR=VAL。
	Env []string

	// Flags 是传递给代理配置创建的标志。
	Flags uint32

	// InitialCols 和 InitialRows 设置初始的列数和行数。
	InitialCols uint32
	InitialRows uint32
}

// WinPTY 表示一个 Windows Pseudo-TTY 对象。
type WinPTY struct {
	StdIn  *os.File // 标准输入流。
	StdOut *os.File // 标准输出流。

	wp          uintptr // winpty 的句柄。
	childHandle uintptr // 子进程的句柄。
	closed      bool    // 是否已关闭。
}

// Read 实现了 io.Reader 接口，从标准输出流读取数据。
func (wp *WinPTY) Read(b []byte) (n int, err error) {
	return wp.StdOut.Read(b)
}

// Write 实现了 io.Writer 接口，向标准输入流写入数据。
func (wp *WinPTY) Write(p []byte) (n int, err error) {
	return wp.StdIn.Write(p)
}

// GetErrorMessage 根据错误指针获取错误消息。
func GetErrorMessage(ptr uintptr) string {
	msgPtr, _, _ := winpty_error_msg.Call(ptr)
	if msgPtr == uintptr(0) {
		return "Unknown Error"
	}

	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(msgPtr)))
}

// UTF16PtrFromStringArray 将字符串数组转换为 UTF-16 指针。
func UTF16PtrFromStringArray(s []string) (*uint16, error) {
	var r []uint16

	for _, ss := range s {
		a, err := syscall.UTF16FromString(ss)
		if err != nil {
			return nil, err
		}

		r = append(r, a...)
	}

	r = append(r, 0)

	return &r[0], nil
}

// OpenWithOptions 根据配置选项打开一个 WinPTY。
func OpenWithOptions(options Options) (*WinPTY, error) {
	err := loadWinPty()
	if err != nil {
		return nil, err
	}

	// 创建代理配置。
	agentCfg, err := createAgentCfg(options.Flags)
	if err != nil {
		return nil, err
	}

	// 如果未设置初始大小，则默认为 40x40。
	if options.InitialCols <= 0 {
		options.InitialCols = 40
	}
	if options.InitialRows <= 0 {
		options.InitialRows = 40
	}
	winpty_config_set_initial_size.Call(agentCfg, uintptr(options.InitialCols), uintptr(options.InitialRows))

	var openErr uintptr
	defer winpty_error_free.Call(openErr)
	// 打开 winpty。
	wp, _, _ := winpty_open.Call(agentCfg, uintptr(unsafe.Pointer(openErr)))

	if wp == uintptr(0) {
		return nil, fmt.Errorf("Error Launching WinPTY agent, %s", GetErrorMessage(openErr))
	}

	winpty_config_free.Call(agentCfg)

	// 获取标准输入和输出的名称。
	stdin_name, _, _ := winpty_conin_name.Call(wp)
	stdout_name, _, _ := winpty_conout_name.Call(wp)

	obj := &WinPTY{}
	// 打开标准输入流。
	stdin_handle, err := syscall.CreateFile((*uint16)(unsafe.Pointer(stdin_name)), syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("Error getting stdin handle. %s", err)
	}
	obj.StdIn = os.NewFile(uintptr(stdin_handle), "stdin")
	// 打开标准输出流。
	stdout_handle, err := syscall.CreateFile((*uint16)(unsafe.Pointer(stdout_name)), syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("Error getting stdout handle. %s", err)
	}
	obj.StdOut = os.NewFile(uintptr(stdout_handle), "stdout")

	// 创建启动配置。
	spawnCfg, err := createSpawnCfg(WINPTY_SPAWN_FLAG_AUTO_SHUTDOWN, options.AppName, options.Command, options.Dir, options.Env)
	if err != nil {
		return nil, err
	}

	var (
		spawnErr  uintptr
		lastError *uint32
	)
	// 启动进程。
	spawnRet, _, _ := winpty_spawn.Call(wp, spawnCfg, uintptr(unsafe.Pointer(&obj.childHandle)), uintptr(0), uintptr(unsafe.Pointer(lastError)), uintptr(unsafe.Pointer(spawnErr)))
	winpty_spawn_config_free.Call(spawnCfg)
	defer winpty_error_free.Call(spawnErr)

	if spawnRet == 0 {
		return nil, fmt.Errorf("Error spawning process...")
	} else {
		obj.wp = wp
		return obj, nil
	}
}

// SetSize 设置 winpty 的大小。
func (obj *WinPTY) SetSize(ws_col, ws_row uint32) {
	if ws_col == 0 || ws_row == 0 {
		return
	}
	winpty_set_size.Call(obj.wp, uintptr(ws_col), uintptr(ws_row), uintptr(0))
}

// Close 关闭 winpty 并释放相关资源。
func (obj *WinPTY) Close() {
	if obj.closed {
		return
	}

	winpty_free.Call(obj.wp)

	obj.StdIn.Close()
	obj.StdOut.Close()

	syscall.CloseHandle(syscall.Handle(obj.childHandle))

	obj.closed = true
}

// GetProcHandle 获取子进程的句柄。
func (obj *WinPTY) GetProcHandle() uintptr {
	return obj.childHandle
}
