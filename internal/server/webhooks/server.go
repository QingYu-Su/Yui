package webhooks

import (
	"bytes"         // 用于操作字节缓冲区
	"crypto/tls"    // 用于处理 TLS 配置
	"encoding/json" // 用于 JSON 编码和解码
	"log"           // 用于记录日志
	"time"          // 用于处理时间相关操作

	"net/http" // 用于发送 HTTP 请求

	"github.com/QingYu-Su/Yui/internal/server/data"      // 导入数据模块，用于操作数据库
	"github.com/QingYu-Su/Yui/internal/server/observers" // 导入观察者模块，用于处理客户端状态消息
)

// StartWebhooks 启动 Webhook 消息发送服务
func StartWebhooks() {
	// 创建一个通道，用于接收客户端状态消息
	messages := make(chan observers.ClientState)

	// 注册一个回调函数到观察者对象，当有新的客户端状态消息时，将其发送到通道中
	observers.ConnectionState.Register(func(message observers.ClientState) {
		messages <- message
	})

	// 启动一个 goroutine，用于处理通道中的消息
	go func() {
		for msg := range messages {
			// 对每个消息启动一个新的 goroutine，以并发方式处理
			go func(msg observers.ClientState) {
				// 将客户端状态消息序列化为 JSON 格式
				fullBytes, err := msg.Json()
				if err != nil {
					log.Println("Bad webhook message: ", err) // 如果序列化失败，记录日志并返回
					return
				}

				// 创建一个包装结构，包含完整的 JSON 数据和简要摘要
				wrapper := struct {
					Full string // 完整的 JSON 数据
					Text string `json:"text"` // 简要摘要
				}{
					Full: string(fullBytes),
					Text: msg.Summary(),
				}

				// 将包装结构序列化为 JSON 格式
				webhookMessage, _ := json.Marshal(wrapper)

				// 从数据库中获取所有 Webhook 配置
				recipients, err := data.GetAllWebhooks()
				if err != nil {
					log.Println("error fetching webhooks: ", err) // 如果获取失败，记录日志并返回
					return
				}

				// 遍历所有 Webhook 配置，发送消息
				for _, webhook := range recipients {
					// 配置 HTTP 客户端的 TLS 设置
					tr := &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: webhook.CheckTLS},
					}

					// 创建 HTTP 客户端，设置超时时间为 2 秒
					client := http.Client{
						Timeout:   2 * time.Second,
						Transport: tr,
					}

					// 创建一个字节缓冲区，包含要发送的 JSON 数据
					buff := bytes.NewBuffer(webhookMessage)
					// 发送 POST 请求到 Webhook 的 URL
					_, err := client.Post(webhook.URL, "application/json", buff)
					if err != nil {
						log.Printf("Error sending webhook '%s': %s\n", webhook.URL, err) // 如果发送失败，记录日志
					}
				}
			}(msg)
		}
	}()
}
