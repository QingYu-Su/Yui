package data

import (
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Download 数据表结构，用于存储下载文件的相关信息
type Download struct {
	gorm.Model // GORM 的默认模型，包含 ID、CreatedAt、UpdatedAt、DeletedAt 等字段

	// 下载文件的 URL 路径，唯一标识
	UrlPath string `gorm:"unique"`

	// 回调地址，用于在下载完成后发送通知
	CallbackAddress string

	// 文件保存路径
	FilePath string

	// 日志级别
	LogLevel string

	// Go 构建目标的操作系统
	Goos string

	// Go 构建目标的架构
	Goarch string

	// Go 构建目标的 ARM 架构版本
	Goarm string

	// 文件类型
	FileType string

	// 下载次数
	Hits int

	// 文件版本
	Version string

	// 文件大小（单位：字节）
	FileSize float64

	// 是否使用 Host Header 生成模板
	UseHostHeader bool

	// 下载文件的工作目录
	WorkingDirectory string
}

// CreateDownload 创建一个新的下载记录
func CreateDownload(file Download) error {
	// 在数据库中创建一个新的 Download 记录
	return db.Create(&file).Error
}

// GetDownload 根据 URL 路径获取下载记录，并更新下载次数
func GetDownload(urlPath string) (Download, error) {
	var download Download // 定义一个 Download 类型的变量用于存储查询结果
	// 查询 URL 路径匹配的下载记录
	if err := db.Where("url_path = ?", urlPath).First(&download).Error; err != nil {
		return download, err
	}

	// 更新下载次数（每次访问时加 1）
	if err := db.Model(&Download{}).Where("url_path = ?", urlPath).Update("hits", download.Hits+1).Error; err != nil {
		return download, err
	}

	return download, nil
}

// ListDownloads 根据过滤条件列出所有匹配的下载记录,返回url和downdload的map
// 过滤条件可以是路径、os、arch和arm的组合
func ListDownloads(filter string) (matchingFiles map[string]Download, err error) {
	// 验证过滤条件是否符合文件路径匹配规则
	_, err = filepath.Match(filter, "")
	if err != nil {
		return nil, fmt.Errorf("filter is not well formed")
	}

	// 初始化一个空的映射，用于存储匹配的下载记录
	matchingFiles = make(map[string]Download)

	// 查询数据库中所有的下载记录
	var downloads []Download
	if err := db.Find(&downloads).Error; err != nil {
		return nil, err
	}

	// 遍历所有下载记录，根据过滤条件筛选
	for _, file := range downloads {
		if filter == "" { // 如果过滤条件为空，直接添加到结果中
			matchingFiles[file.UrlPath] = file
			continue
		}

		// 根据 URL 路径匹配
		if match, _ := filepath.Match(filter, file.UrlPath); match {
			matchingFiles[file.UrlPath] = file
			continue
		}

		// 根据 Goos 匹配
		if match, _ := filepath.Match(filter, file.Goos); match {
			matchingFiles[file.UrlPath] = file
			continue
		}

		// 根据 Goarch 和 Goarm 组合匹配
		if match, _ := filepath.Match(filter, file.Goarch+file.Goarm); match {
			matchingFiles[file.UrlPath] = file
			continue
		}
	}

	return
}

// DeleteDownload 根据 URL 路径删除下载记录，并删除对应的文件
func DeleteDownload(key string) error {
	// 定义一个 Download 类型的变量用于存储查询结果
	var download Download
	// 从数据库中删除 URL 路径匹配的下载记录，并返回删除的记录
	if err := db.Unscoped().Clauses(clause.Returning{}).Where("url_path = ?", key).Delete(&download).Error; err != nil {
		return err
	}

	// 删除对应的文件
	return os.Remove(download.FilePath)
}
