//go:build windows

package wauth

import (
	"encoding/base64" // 用于Base64编码和解码
	"fmt"             // 格式化输入输出
	"log"             // 日志记录
	"strings"         // 字符串操作
	"syscall"         // 提供对底层操作系统功能的访问
	"unsafe"          // 提供对底层内存操作的支持
)

var (
	// 加载Windows系统中的secur32.dll动态链接库，该库包含安全相关的函数。
	modsecur32 = syscall.NewLazyDLL("secur32.dll")

	// 定义secur32.dll中需要调用的函数
	procAcquireCredentialsHandleW  = modsecur32.NewProc("AcquireCredentialsHandleW")  // 获取凭证句柄
	procFreeCredentialsHandle      = modsecur32.NewProc("FreeCredentialsHandle")      // 释放凭证句柄
	procInitializeSecurityContextW = modsecur32.NewProc("InitializeSecurityContextW") // 初始化安全上下文

	// 定义错误码与错误信息的映射表，用于将Windows安全API返回的错误码转换为可读的错误信息。
	errors = map[int64]string{
		0x80090300: "SEC_E_INSUFFICIENT_MEMORY",         // 内存不足
		0x80090304: "SEC_E_INTERNAL_ERROR",              // 内部错误
		0x8009030E: "SEC_E_NO_CREDENTIALS",              // 没有凭证
		0x80090306: "SEC_E_NOT_OWNER",                   // 不是所有者
		0x80090305: "SEC_E_SECPKG_NOT_FOUND",            // 安全包未找到
		0x8009030D: "SEC_E_UNKNOWN_CREDENTIALS",         // 未知凭证
		0x80090301: "SEC_E_INVALID_HANDLE",              // 无效的句柄
		0x80090308: "SEC_E_INVALID_TOKEN",               // 无效的令牌
		0x8009030C: "SEC_E_LOGON_DENIED",                // 登录被拒绝
		0x80090311: "SEC_E_NO_AUTHENTICATING_AUTHORITY", // 没有认证机构
		0x80090303: "SEC_E_TARGET_UNKNOWN",              // 目标未知
		0x80090302: "SEC_E_UNSUPPORTED_FUNCTION",        // 不支持的函数
		0x80090322: "SEC_E_WRONG_PRINCIPAL",             // 错误的主体
		0x00090314: "SEC_I_COMPLETE_AND_CONTINUE",       // 需要完成并继续
		0x00090312: "SEC_I_CONTINUE_NEEDED",             // 需要继续
		0x00090313: "SEC_I_COMPLETE_NEEDED",             // 需要完成
	}
)

// orPanic 是一个辅助函数，用于在发生错误时直接抛出panic。
// 如果传入的错误不为nil，则触发panic。
func orPanic(err error) {
	if err != nil {
		panic(err)
	}
}

// AcquireCredentialsHandle 是一个封装函数，用于调用Windows的AcquireCredentialsHandleW函数。
// 它用于获取安全凭证句柄，该句柄可用于后续的安全操作。
func AcquireCredentialsHandle(principal *uint16, pckge *uint16, credentialuse uint32, logonid *uint64, authdata *byte, getkeyfn *byte, getkeyargument *byte, credential *CredHandle, expiry *TimeStamp) (status SECURITY_STATUS) {
	// 调用AcquireCredentialsHandleW函数，通过syscall.Syscall9进行底层调用。
	r0, _, _ := syscall.Syscall9(procAcquireCredentialsHandleW.Addr(), 9,
		uintptr(unsafe.Pointer(principal)),      // 主体名称
		uintptr(unsafe.Pointer(pckge)),          // 安全包名称
		uintptr(credentialuse),                  // 凭证使用方式
		uintptr(unsafe.Pointer(logonid)),        // 登录ID
		uintptr(unsafe.Pointer(authdata)),       // 认证数据
		uintptr(unsafe.Pointer(getkeyfn)),       // 获取密钥的回调函数
		uintptr(unsafe.Pointer(getkeyargument)), // 回调函数的参数
		uintptr(unsafe.Pointer(credential)),     // 凭证句柄
		uintptr(unsafe.Pointer(expiry)))         // 凭证过期时间
	status = SECURITY_STATUS(r0) // 将返回值转换为SECURITY_STATUS类型
	return
}

// FreeCredentialsHandle 是一个封装函数，用于调用Windows的FreeCredentialsHandle函数。
// 它用于释放安全凭证句柄，释放资源。
func FreeCredentialsHandle(credential *CredHandle) (status SECURITY_STATUS) {
	// 调用FreeCredentialsHandle函数，通过syscall.Syscall进行底层调用。
	r0, _, _ := syscall.Syscall(procFreeCredentialsHandle.Addr(), 1,
		uintptr(unsafe.Pointer(credential)), // 凭证句柄
		0,                                   // 保留参数
		0)                                   // 保留参数
	status = SECURITY_STATUS(r0) // 将返回值转换为SECURITY_STATUS类型
	return
}

// InitializeSecurityContext 是一个封装函数，用于调用Windows的InitializeSecurityContextW函数。
// 它用于初始化安全上下文，建立安全连接。
func InitializeSecurityContext(credential *CredHandle, context *CtxtHandle, targetname *uint16, contextreq uint32, reserved1 uint32, targetdatarep uint32, input *SecBufferDesc, reserved2 uint32, newcontext *CtxtHandle, output *SecBufferDesc, contextattr *uint32, expiry *TimeStamp) (status SECURITY_STATUS) {
	// 调用InitializeSecurityContextW函数，通过syscall.Syscall12进行底层调用。
	r0, _, _ := syscall.Syscall12(procInitializeSecurityContextW.Addr(), 12,
		uintptr(unsafe.Pointer(credential)),  // 凭证句柄
		uintptr(unsafe.Pointer(context)),     // 当前安全上下文
		uintptr(unsafe.Pointer(targetname)),  // 目标名称
		uintptr(contextreq),                  // 上下文要求
		uintptr(reserved1),                   // 保留参数
		uintptr(targetdatarep),               // 目标数据表示
		uintptr(unsafe.Pointer(input)),       // 输入安全缓冲区
		uintptr(reserved2),                   // 保留参数
		uintptr(unsafe.Pointer(newcontext)),  // 新的安全上下文
		uintptr(unsafe.Pointer(output)),      // 输出安全缓冲区
		uintptr(unsafe.Pointer(contextattr)), // 上下文属性
		uintptr(unsafe.Pointer(expiry)))      // 上下文过期时间
	status = SECURITY_STATUS(r0) // 将返回值转换为SECURITY_STATUS类型
	return
}

// 定义与安全凭证和上下文相关的常量，在创建安全凭证和上下文时需要用到
const (
	// 凭证使用方式的标志
	SECPKG_CRED_AUTOLOGON_RESTRICTED = 0x00000010 // 自动登录受限
	SECPKG_CRED_BOTH                 = 0x00000003 // 用于双向通信
	SECPKG_CRED_INBOUND              = 0x00000001 // 用于入站通信
	SECPKG_CRED_OUTBOUND             = 0x00000002 // 用于出站通信
	SECPKG_CRED_PROCESS_POLICY_ONLY  = 0x00000020 // 仅用于进程策略

	// 安全状态和请求标志
	SEC_E_OK                = 0x00000000 // 操作成功
	ISC_REQ_ALLOCATE_MEMORY = 0x00000100 // 请求分配内存
	ISC_REQ_CONNECTION      = 0x00000800 // 请求建立连接
	ISC_REQ_INTEGRITY       = 0x00010000 // 请求完整性保护
	SECURITY_NATIVE_DREP    = 0x00000010 // 本地数据表示
	SECURITY_NETWORK_DREP   = 0x00000000 // 网络数据表示
	ISC_REQ_CONFIDENTIALITY = 0x00000010 // 请求保密性保护
	ISC_REQ_REPLAY_DETECT   = 0x00000004 // 请求重放检测

	// 安全缓冲区类型
	SECBUFFER_TOKEN = 2 // 安全令牌
)

// 定义安全状态类型
type SECURITY_STATUS int32

// 判断安全状态是否为错误
func (s SECURITY_STATUS) IsError() bool {
	return s < 0
}

// 判断安全状态是否为信息性状态
func (s SECURITY_STATUS) IsInformation() bool {
	return s > 0
}

// 定义错误类型
type Error int32

// 返回错误的字符串表示
func (e Error) Error() string {
	return fmt.Sprintf("error #%x", uint32(e))
}

// 定义凭证句柄结构
type CredHandle struct {
	Lower *uint32 // 低32位句柄
	Upper *uint32 // 高32位句柄
}

// 定义上下文句柄结构
type CtxtHandle struct {
	Lower *uint32 // 低32位句柄
	Upper *uint32 // 高32位句柄
}

// 定义安全缓冲区结构
type SecBuffer struct {
	Count  uint32 // 缓冲区大小
	Type   uint32 // 缓冲区类型
	Buffer *byte  // 缓冲区指针
}

// 定义安全缓冲区描述符结构
type SecBufferDesc struct {
	Version uint32     // 版本号
	Count   uint32     // 缓冲区数量
	Buffers *SecBuffer // 缓冲区数组指针
}

// TimeStamp 是一个别名，表示时间戳（仅在Windows上可用）
type TimeStamp syscall.Filetime

// 定义凭证结构
type Credentials struct {
	Handle CredHandle // 凭证句柄
}

// AcquireCredentials 函数用于获取安全凭证
func AcquireCredentials(username string) (*Credentials, SECURITY_STATUS, error) {
	var h CredHandle // 凭证句柄
	// 调用 AcquireCredentialsHandle 函数获取凭证句柄
	s := AcquireCredentialsHandle(nil, syscall.StringToUTF16Ptr("Negotiate"),
		SECPKG_CRED_OUTBOUND, nil, nil, nil, nil, &h, nil)
	if s.IsError() {
		// 如果发生错误，返回 nil 和错误状态
		return nil, s, Error(s)
	}
	// 返回凭证结构和状态
	return &Credentials{Handle: h}, s, nil
}

// Close 方法用于关闭凭证句柄
func (c *Credentials) Close() error {
	// 调用 FreeCredentialsHandle 函数释放凭证句柄
	s := FreeCredentialsHandle(&c.Handle)
	if s.IsError() {
		// 如果发生错误，返回错误
		return Error(s)
	}
	return nil
}

// Context 表示安全上下文的结构
type Context struct {
	Handle     CtxtHandle    // 安全上下文句柄
	BufferDesc SecBufferDesc // 安全缓冲区描述符
	Buffer     SecBuffer     // 安全缓冲区
	Data       [4096]byte    // 用于存储安全令牌的数据缓冲区
	Attrs      uint32        // 上下文属性
}

// NewContext 方法用于初始化一个新的安全上下文
func (c *Credentials) NewContext(target string) (*Context, SECURITY_STATUS, error) {
	var x Context                        // 创建一个新的上下文结构
	x.Buffer.Buffer = &x.Data[0]         // 将缓冲区指针指向Data数组的起始位置
	x.Buffer.Count = uint32(len(x.Data)) // 设置缓冲区大小为Data数组的长度
	x.Buffer.Type = SECBUFFER_TOKEN      // 设置缓冲区类型为安全令牌
	x.BufferDesc.Count = 1               // 设置缓冲区描述符中的缓冲区数量为1
	x.BufferDesc.Buffers = &x.Buffer     // 将缓冲区描述符指向Buffer

	// 调用 InitializeSecurityContext 函数初始化安全上下文
	s := InitializeSecurityContext(&c.Handle, nil, syscall.StringToUTF16Ptr(target),
		ISC_REQ_CONFIDENTIALITY|ISC_REQ_REPLAY_DETECT|ISC_REQ_CONNECTION, // 请求的上下文要求
		0, SECURITY_NETWORK_DREP, nil, // 保留参数和目标数据表示
		0, &x.Handle, &x.BufferDesc, &x.Attrs, nil) // 输出参数

	// 检查是否发生错误
	if s.IsError() {
		return nil, s, Error(s)
	}
	return &x, s, nil // 返回初始化的上下文结构和状态
}

// GetAuthorizationHeader 函数用于生成授权头
func GetAuthorizationHeader(proxyURL string) string {
	// Acquire credentials
	cred, status, err := AcquireCredentials("") // 获取安全凭证
	if err != nil {
		log.Printf("AcquireCredentials failed: %v %s", err, errors[int64(status)]) // 如果失败，记录错误信息
	}
	defer cred.Close()                                            // 确保在函数退出时释放凭证
	log.Printf("AcquireCredentials success: status=0x%x", status) // 记录凭证获取成功的信息

	// Initialize Context
	tgt := "http/" + strings.ToUpper(strings.Replace(strings.Split(proxyURL, ":")[1], "//", "", -1)) // 构造目标SPN
	log.Printf("Requesting for context against SPN %s", tgt)                                         // 记录请求上下文的目标SPN
	ctxt, status, err := cred.NewContext(tgt)                                                        // 初始化新的安全上下文

	if err != nil {
		log.Printf("NewContext failed: %v", err) // 如果失败，记录错误信息
	}
	log.Printf("NewContext success: status=0x%x errorcode=%s", status, errors[int64(status)]) // 记录上下文初始化成功的信息

	// Generate the Authorization header
	// 将安全令牌数据进行Base64编码，并生成授权头
	headerstr := "Negotiate " + base64.StdEncoding.EncodeToString(ctxt.Data[0:ctxt.Buffer.Count])
	log.Printf("Generated header %s", headerstr) // 记录生成的授权头

	return headerstr // 返回授权头
}
