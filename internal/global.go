package internal // 定义包名为 internal，通常用于项目内部的私有包

import (
	"crypto/ed25519"  // 导入 ed25519 加密算法库，用于生成密钥
	"crypto/rand"     // 导入随机数生成器，用于生成随机数据
	"crypto/sha1"     // 导入 SHA1 哈希算法库
	"crypto/sha256"   // 导入 SHA256 哈希算法库
	"crypto/x509"     // 导入 x509 证书处理库
	"encoding/binary" // 导入二进制编码库，用于处理字节序
	"encoding/hex"    // 导入十六进制编码库
	"encoding/pem"    // 导入 PEM 编码库，用于处理 PEM 格式的密钥和证书
	"fmt"             // 导入格式化输入输出库
	"log"             // 导入日志库
	"net"             // 导入网络库

	"golang.org/x/crypto/ssh" // 导入 SSH 库，用于处理 SSH 协议
)

// 全局变量
var (
	Version      string             // 版本信息，用于标识程序版本
	ConsoleLabel string = "catcher" // 控制台标签，默认值为 "catcher"
)

// ShellStruct 定义了一个结构体，用于存储命令信息
type ShellStruct struct {
	Cmd string // 命令字符串
}

// RemoteForwardRequest 定义了一个结构体，用于表示远程端口转发请求
type RemoteForwardRequest struct {
	BindAddr string // 绑定地址
	BindPort uint32 // 绑定端口
}

// String 方法实现了 RemoteForwardRequest 的字符串表示形式
func (r *RemoteForwardRequest) String() string {
	return net.JoinHostPort(r.BindAddr, fmt.Sprintf("%d", r.BindPort))
}

// ChannelOpenDirectMsg 定义了一个结构体，用于表示直接通道打开消息
type ChannelOpenDirectMsg struct {
	Raddr string // 目标地址
	Rport uint32 // 目标端口
	Laddr string // 源地址
	Lport uint32 // 源端口
}

// GeneratePrivateKey 生成一个私钥，并将其转换为 PEM 格式
func GeneratePrivateKey() ([]byte, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	// 将生成的 ed25519 私钥转换为 PKCS8 格式
	bytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}

	// 将私钥编码为 PEM 格式
	privatePem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: bytes,
		},
	)

	return privatePem, nil
}

// FingerprintSHA1Hex 计算公钥的 SHA1 指纹，并将其转换为十六进制字符串
func FingerprintSHA1Hex(pubKey ssh.PublicKey) string {
	shasum := sha1.Sum(pubKey.Marshal())
	fingerPrint := hex.EncodeToString(shasum[:])
	return fingerPrint
}

// FingerprintSHA256Hex 计算公钥的 SHA256 指纹，并将其转换为十六进制字符串
func FingerprintSHA256Hex(pubKey ssh.PublicKey) string {
	shasum := sha256.Sum256(pubKey.Marshal())
	fingerPrint := hex.EncodeToString(shasum[:])
	return fingerPrint
}

// SendRequest 发送 SSH 请求
func SendRequest(req ssh.Request, sshChan ssh.Channel) (bool, error) {
	return sshChan.SendRequest(req.Type, req.WantReply, req.Payload)
}

// PtyReq 定义了一个结构体，用于表示伪终端请求
type PtyReq struct {
	Term          string // 终端类型
	Columns, Rows uint32 // 列数和行数
	Width, Height uint32 // 宽度和高度
	Modes         string // 模式
}

// ClientInfo 定义了一个结构体，用于存储客户端信息
type ClientInfo struct {
	Username string // 用户名
	Hostname string // 主机名
	GoArch   string // Go 架构
	GoOS     string // Go 操作系统
}

// ParsePtyReq 解析伪终端请求数据
func ParsePtyReq(req []byte) (out PtyReq, err error) {
	err = ssh.Unmarshal(req, &out)
	return out, err
}

// ParseDims 解析终端尺寸（宽度和高度）
func ParseDims(b []byte) (uint32, uint32) {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return w, h
}

// RandomString 生成随机字符串
func RandomString(length int) (string, error) {
	randomData := make([]byte, length)
	_, err := rand.Read(randomData)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(randomData), nil
}

// DiscardChannels 丢弃无效的 SSH 通道请求
func DiscardChannels(sshConn ssh.Conn, chans <-chan ssh.NewChannel) {
	for newChannel := range chans {
		t := newChannel.ChannelType()

		// 拒绝不支持的通道类型
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unsupported channel type: %s", t))
		log.Printf("Client %s (%s) sent invalid channel type '%s'\n", sshConn.RemoteAddr(), sshConn.ClientVersion(), t)
	}
}
