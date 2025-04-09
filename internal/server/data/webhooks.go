package data

import (
	"errors"  // 用于定义和处理自定义错误
	"fmt"     // 用于格式化输出
	"net"     // 用于网络相关的操作，如域名解析
	"net/url" // 用于解析和操作 URL

	"gorm.io/gorm" // 用于操作数据库
)

// Webhook 数据表结构，用于存储 Webhook 的相关信息
type Webhook struct {
	gorm.Model        // GORM 的默认模型，包含 ID、CreatedAt、UpdatedAt、DeletedAt 等字段
	URL        string // Webhook 的 URL 地址
	CheckTLS   bool   // 是否检查 TLS 证书
}

// CreateWebhook 创建一个新的 Webhook 记录
func CreateWebhook(newUrl string, checktls bool) (string, error) {
	// 解析输入的 URL 字符串
	u, err := url.Parse(newUrl)
	if err != nil {
		return "", err // 如果解析失败，返回错误
	}

	// 检查 URL 的协议是否为 http 或 https
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("only http and https schemes are supported: supplied scheme: " + u.Scheme)
	}

	// 解析 URL 的主机名对应的 IP 地址
	addresses, err := net.LookupIP(u.Hostname())
	if err != nil {
		return "", fmt.Errorf("unable to lookup hostname '%s': %s", u.Hostname(), err)
	}

	// 检查是否找到了有效的 IP 地址
	if len(addresses) == 0 {
		return "", fmt.Errorf("no addresses found for '%s': %s", u.Hostname(), err)
	}

	// 创建一个新的 Webhook 实例
	webhook := Webhook{
		URL:      newUrl,
		CheckTLS: checktls,
	}

	// 将 Webhook 记录添加到数据库
	if err := db.Create(&webhook).Error; err != nil {
		return "", fmt.Errorf("failed to create webhook in the database: %s", err)
	}

	// 返回解析后的 URL 字符串
	return u.String(), nil
}

// GetAllWebhooks 获取数据库中所有的 Webhook 记录
func GetAllWebhooks() ([]Webhook, error) {
	var webhooks []Webhook // 定义一个切片用于存储查询结果
	// 查询数据库中的所有 Webhook 记录
	if err := db.Find(&webhooks).Error; err != nil {
		return nil, err // 如果查询失败，返回错误
	}
	return webhooks, nil // 返回查询结果
}

// DeleteWebhook 根据 URL 删除 Webhook 记录
func DeleteWebhook(url string) error {
	// 在数据库中删除 URL 匹配的 Webhook 记录
	return db.Where("url = ?", url).Delete(&Webhook{}).Error
}
