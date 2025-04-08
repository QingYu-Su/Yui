package protocols

// Type 是一个自定义类型，用于表示协议类型。
// 它基于 string 类型，方便在代码中使用字符串来表示不同的协议。
type Type string

const (
	// Wrappers/Transports
	// 以下常量定义了协议的包装/传输层类型
	Websockets Type = "ws"      // 表示 WebSocket 协议
	HTTP       Type = "polling" // 表示 HTTP 长轮询协议
	TLS        Type = "tls"     // 表示 TLS 协议

	// Final control/data channel
	// 以下常量定义了最终的控制/数据通道协议类型
	HTTPDownload Type = "download"     // 表示 HTTP 下载协议
	TCPDownload  Type = "downloadBash" // 表示 TCP 下载协议（可能是特定的 Bash 脚本下载方式）

	// 其他协议类型
	C2      Type = "ssh"     // 表示 SSH 协议（命令与控制协议）
	Invalid Type = "invalid" // 表示无效协议
)

// FullyUnwrapped 函数用于判断当前协议是否是“完全展开”的。
// 参数：
//   - currentProtocol：当前协议类型
//
// 返回值：
//   - bool：如果当前协议是完全展开的，返回 true；否则返回 false
func FullyUnwrapped(currentProtocol Type) bool {
	// 判断当前协议是否是最终的控制/数据通道协议之一
	return currentProtocol == C2 || currentProtocol == HTTPDownload || currentProtocol == TCPDownload
}
