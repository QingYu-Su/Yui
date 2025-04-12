package shellscripts

import (
	"bytes"         // 提供字节缓冲区操作
	"embed"         // 提供嵌入文件功能
	"io"            // 提供输入输出操作
	"text/template" // 提供文本模板解析和执行功能
)

// 嵌入 templates 文件夹下的所有文件

//go:embed templates/*
var shellTemplates embed.FS // 声明一个嵌入文件系统变量，存储嵌入的模板文件

// Args 定义了模板渲染所需的参数结构体
type Args struct {
	Protocol         string // 协议类型，如 http、ssh 等
	Host             string // 主机地址
	Port             string // 端口号
	Name             string // 名称
	Arch             string // 架构
	OS               string // 操作系统
	WorkingDirectory string // 工作目录
}

// MakeTemplate 根据给定的参数和模板扩展名生成模板内容
func MakeTemplate(attributes Args, extension string) ([]byte, error) {
	// 打开嵌入的模板文件
	file, err := shellTemplates.Open("templates/" + extension)
	if err != nil {
		return nil, err // 如果打开文件失败，返回错误
	}

	// 读取模板文件的全部内容
	t, err := io.ReadAll(file)
	if err != nil {
		return nil, err // 如果读取文件失败，返回错误
	}

	// 解析模板内容，创建一个新的模板对象
	template, err := template.New("shell").Parse(string(t))
	if err != nil {
		return nil, err // 如果解析模板失败，返回错误
	}

	// 创建一个字节缓冲区，用于存储模板渲染后的结果
	var b bytes.Buffer
	// 执行模板渲染，将参数传递给模板
	err = template.Execute(&b, attributes)
	if err != nil {
		return nil, err // 如果模板渲染失败，返回错误
	}

	// 返回渲染后的模板内容
	return b.Bytes(), nil
}
