package observer

import (
	"crypto/rand"  // 导入用于生成随机数据的包
	"encoding/hex" // 导入用于将字节数据编码为十六进制字符串的包
	"sync"         // 导入用于同步操作的包，如读写锁
)

// random 函数用于生成指定长度的随机十六进制字符串
func random(length int) (string, error) {
	randomData := make([]byte, length) // 创建一个长度为 length 的字节切片用于存储随机数据
	_, err := rand.Read(randomData)    // 使用 crypto/rand 包生成随机数据填充 randomData
	if err != nil {
		return "", err // 如果生成随机数据失败，返回错误
	}

	return hex.EncodeToString(randomData), nil // 将随机数据编码为十六进制字符串并返回
}

// observer 是一个泛型结构体，用于实现观察者模式
// T 是泛型参数，表示通知消息的类型
type observer[T any] struct {
	sync.RWMutex                    // 内嵌读写锁，用于同步操作
	clients      map[string]func(T) // 存储观察者的回调函数，key 是观察者 ID，value 是回调函数
}

// Register 方法用于注册一个观察者
// 参数 f 是观察者的回调函数，当通知发生时会调用此函数
// 返回一个唯一的观察者 ID，用于后续的注销操作
func (o *observer[T]) Register(f func(T)) (id string) {
	o.Lock()         // 加写锁，保证线程安全
	defer o.Unlock() // 确保在函数返回时释放锁

	id, _ = random(10) // 生成一个长度为 10 的随机字符串作为观察者 ID

	o.clients[id] = f // 将观察者的回调函数存储到 clients 中

	return id // 返回观察者 ID
}

// Deregister 方法用于注销一个观察者
// 参数 id 是要注销的观察者的 ID
func (o *observer[T]) Deregister(id string) {
	o.Lock()         // 加写锁，保证线程安全
	defer o.Unlock() // 确保在函数返回时释放锁

	delete(o.clients, id) // 从 clients 中删除指定 ID 的观察者
}

// Notify 方法用于通知所有注册的观察者
// 参数 message 是要传递给观察者的通知消息
func (o *observer[T]) Notify(message T) {
	o.RLock()         // 加读锁，允许多个读操作同时进行
	defer o.RUnlock() // 确保在函数返回时释放锁

	for i := range o.clients { // 遍历所有注册的观察者
		go o.clients[i](message) // 使用 goroutine 并发调用观察者的回调函数
	}
}

// New 函数用于创建一个新的 observer 实例
// 返回一个初始化好的 observer 对象
func New[T any]() observer[T] {
	return observer[T]{
		clients: make(map[string]func(T)), // 初始化 clients 为一个空的 map
	}
}
