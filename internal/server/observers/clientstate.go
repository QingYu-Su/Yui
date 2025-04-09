package observers

import (
	"encoding/json" // 用于 JSON 编码和解码
	"fmt"           // 用于格式化输出
	"time"          // 用于处理时间相关操作

	"github.com/QingYu-Su/Yui/pkg/observer" // 导入 observer 包，用于实现观察者模式
)

// ClientState 定义客户端的状态信息
type ClientState struct {
	Status    string    // 客户端的状态（例如 "online" 或 "offline"）
	ID        string    // 客户端的唯一标识符
	IP        string    // 客户端的 IP 地址
	HostName  string    // 客户端的主机名
	Version   string    // 客户端的版本号
	Timestamp time.Time // 客户端状态的时间戳
}

// Summary 返回客户端状态的简要摘要信息
func (cs ClientState) Summary() string {
	// 使用 fmt.Sprintf 格式化字符串，生成摘要信息
	// 格式为：主机名 (ID) 版本号 状态
	return fmt.Sprintf("%s (%s) %s %s", cs.HostName, cs.ID, cs.Version, cs.Status)
}

// Json 将客户端状态信息序列化为 JSON 格式
func (cs ClientState) Json() ([]byte, error) {
	// 使用 json.Marshal 将 ClientState 结构体序列化为 JSON 字节数组
	return json.Marshal(cs)
}

// ConnectionState 是一个全局的观察者对象，用于管理客户端状态的观察者模式
var ConnectionState = observer.New[ClientState]() // 使用 observer 包的 New 函数创建一个 ClientState 类型的观察者对象
