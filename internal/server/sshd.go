package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server/handlers"
	"github.com/QingYu-Su/Yui/internal/server/observers"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/pkg/logger"
	"github.com/fatih/color"
	"golang.org/x/crypto/ssh"
)

// Options 结构体定义了SSH公钥的配置选项
type Options struct {
	AllowList []*net.IPNet // 允许访问的IP地址列表
	DenyList  []*net.IPNet // 拒绝访问的IP地址列表
	Comment   string       // 公钥的注释信息

	Owners []string // 公钥的所有者列表
}

// readPubKeys 从指定路径读取SSH公钥文件并解析为map
// path: 公钥文件路径
// 返回值:
//
//	m: 映射关系，key为公钥字符串，value为对应的Options配置
//	err: 错误信息
func readPubKeys(path string) (m map[string]Options, err error) {
	// 读取公钥文件内容
	authorizedKeysBytes, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("failed to load file %s, err: %v", path, err)
	}

	// 按行分割公钥内容
	keys := bytes.Split(authorizedKeysBytes, []byte("\n"))
	m = map[string]Options{}

	// 遍历每一行公钥
	for i, key := range keys {
		// 去除前后空白字符
		key = bytes.TrimSpace(key)
		if len(key) == 0 {
			continue // 跳过空行
		}

		// 解析公钥行
		pubKey, comment, options, _, err := ssh.ParseAuthorizedKey(key)
		if err != nil {
			return m, fmt.Errorf("unable to parse public key. %s line %d. Reason: %s", path, i+1, err)
		}

		// 初始化Options结构体
		var opts Options
		opts.Comment = comment

		// 处理公钥选项
		for _, o := range options {
			// 按等号分割选项
			parts := strings.Split(o, "=")
			if len(parts) >= 2 {
				switch parts[0] {
				case "from":
					// 解析from选项，处理IP访问控制列表
					deny, allow := ParseFromDirective(parts[1])
					opts.AllowList = append(opts.AllowList, allow...)
					opts.DenyList = append(opts.DenyList, deny...)
				case "owner":
					// 解析owner选项，处理所有者列表
					opts.Owners = ParseOwnerDirective(parts[1])
				}
			}
		}

		// 将公钥和配置存入map
		m[string(ssh.MarshalAuthorizedKey(pubKey))] = opts
	}

	return
}

// ParseOwnerDirective 解析所有者指令字符串
// 参数: owners - 包含所有者列表的字符串，可能被引号包裹
// 返回值: 解析后的所有者字符串切片
func ParseOwnerDirective(owners string) []string {
	// 尝试去除字符串的引号（如"owner1,owner2" -> owner1,owner2）
	unquoted, err := strconv.Unquote(owners)
	if err != nil {
		return nil // 如果去除引号失败，返回nil
	}

	// 按逗号分割字符串并返回结果
	return strings.Split(unquoted, ",")
}

// ParseFromDirective 解析from指令字符串，处理IP地址访问控制
// 参数: addresses - 包含IP地址规则的字符串
// 返回值:
//
//	deny - 拒绝访问的IP网络列表
//	allow - 允许访问的IP网络列表
func ParseFromDirective(addresses string) (deny, allow []*net.IPNet) {
	// 去除字符串两端的引号
	list := strings.Trim(addresses, "\"")

	// 按逗号分割指令
	directives := strings.Split(list, ",")
	for _, directive := range directives {
		if len(directive) > 0 {
			switch directive[0] {
			case '!': // 以!开头的表示拒绝规则
				directive = directive[1:] // 去掉!前缀
				newDenys, err := ParseAddress(directive)
				if err != nil {
					log.Println("Unable to add !", directive, " to denylist: ", err)
					continue
				}
				deny = append(deny, newDenys...) // 添加到拒绝列表
			default: // 其他情况表示允许规则
				newAllowOnlys, err := ParseAddress(directive)
				if err != nil {
					log.Println("Unable to add ", directive, " to allowlist: ", err)
					continue
				}
				allow = append(allow, newAllowOnlys...) // 添加到允许列表
			}
		}
	}

	return
}

// ParseAddress 解析地址字符串并返回对应的CIDR列表
// 参数: address - 要解析的地址字符串，可以是通配符、CIDR、IP或域名
// 返回值:
//
//	cidr - 解析后的CIDR列表
//	err - 错误信息
func ParseAddress(address string) (cidr []*net.IPNet, err error) {
	// 处理通配符(*)的情况
	if len(address) > 0 && address[0] == '*' {
		// 创建匹配所有IPv4地址的CIDR
		_, all, _ := net.ParseCIDR("0.0.0.0/0")
		// 创建匹配所有IPv6地址的CIDR
		_, allv6, _ := net.ParseCIDR("::/0")
		// 将两个CIDR都添加到返回列表中
		cidr = append(cidr, all, allv6)
		return
	}

	// 尝试将地址解析为CIDR格式
	_, mask, err := net.ParseCIDR(address)
	if err == nil {
		// 解析成功则添加到返回列表
		cidr = append(cidr, mask)
		return
	}

	// 尝试将地址解析为IP地址
	ip := net.ParseIP(address)
	if ip == nil {
		// 处理IP解析失败的情况
		var newcidr net.IPNet
		newcidr.IP = ip
		// 默认IPv4掩码
		newcidr.Mask = net.CIDRMask(32, 32)

		// 如果是IPv6地址则使用IPv6掩码
		if ip.To4() == nil {
			newcidr.Mask = net.CIDRMask(128, 128)
		}

		// 添加到返回列表
		cidr = append(cidr, &newcidr)
		return
	}

	// 尝试将地址作为域名进行DNS解析
	addresses, err := net.LookupIP(address)
	if err != nil {
		return nil, err
	}

	// 为每个解析出的IP地址创建CIDR
	for _, address := range addresses {
		var newcidr net.IPNet
		newcidr.IP = address
		// 默认IPv4掩码
		newcidr.Mask = net.CIDRMask(32, 32)

		// 如果是IPv6地址则使用IPv6掩码
		if address.To4() == nil {
			newcidr.Mask = net.CIDRMask(128, 128)
		}

		// 添加到返回列表
		cidr = append(cidr, &newcidr)
	}

	// 检查是否解析到任何IP地址
	if len(addresses) == 0 {
		return nil, errors.New("Unable to find domains for " + address)
	}

	return
}

// ErrKeyNotInList 定义公钥不在列表中的错误
var ErrKeyNotInList = errors.New("key not found")

// CheckAuth 检查认证信息是否有效
// 参数:
//
//	keysPath - 公钥文件路径
//	publicKey - 客户端提供的公钥
//	src - 客户端IP地址
//	insecure - 是否跳过安全检查
//
// 返回值:
//
//	*ssh.Permissions - 认证通过后的权限信息
//	error - 错误信息
func CheckAuth(keysPath string, publicKey ssh.PublicKey, src net.IP, insecure bool) (*ssh.Permissions, error) {
	// 读取公钥文件
	keys, err := readPubKeys(keysPath)
	if err != nil {
		return nil, ErrKeyNotInList
	}

	var opt Options
	if !insecure {
		// 在安全模式下检查公钥
		var ok bool
		opt, ok = keys[string(ssh.MarshalAuthorizedKey(publicKey))]
		if !ok {
			return nil, ErrKeyNotInList
		}

		// 检查IP是否在拒绝列表中
		for _, deny := range opt.DenyList {
			if deny.Contains(src) {
				return nil, fmt.Errorf("not authorized ip on deny list")
			}
		}

		// 检查IP是否在允许列表中
		safe := len(opt.AllowList) == 0 // 如果没有设置允许列表，默认允许
		for _, allow := range opt.AllowList {
			if allow.Contains(src) {
				safe = true
				break
			}
		}

		if !safe {
			return nil, fmt.Errorf("not authorized not on allow list")
		}
	}

	// 返回权限信息
	return &ssh.Permissions{
		Extensions: map[string]string{
			"comment":   opt.Comment,                            // 公钥注释
			"pubkey-fp": internal.FingerprintSHA1Hex(publicKey), // 公钥指纹
			"owners":    strings.Join(opt.Owners, ","),          // 所有者列表
		},
	}, nil
}

// registerChannelCallbacks 注册SSH通道回调处理函数
// 参数:
//
//	connectionDetails - 连接详情
//	user - 用户信息
//	chans - 传入的SSH通道
//	log - 日志记录器
//	handlers - 通道类型到处理函数的映射
//
// 返回值:
//
//	error - 错误信息
func registerChannelCallbacks(connectionDetails string, user *users.User, chans <-chan ssh.NewChannel, log logger.Logger, handlers map[string]func(connectionDetails string, user *users.User, newChannel ssh.NewChannel, log logger.Logger)) error {
	// 处理每个传入的通道
	for newChannel := range chans {
		t := newChannel.ChannelType()
		log.Info("Handling channel: %s", t)

		// 检查是否有对应的处理函数
		if callBack, ok := handlers[t]; ok {
			// 异步调用处理函数
			go callBack(connectionDetails, user, newChannel, log)
			continue
		}

		// 拒绝不支持的通道类型
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unsupported channel type: %s", t))
		log.Warning("Sent an invalid channel type %q", t)
	}

	return fmt.Errorf("connection terminated")
}

// isDirEmpty 检查指定目录是否为空
// 参数:
//
//	name - 要检查的目录路径
//
// 返回值:
//
//	bool - 如果目录为空返回true，否则返回false
func isDirEmpty(name string) bool {
	// 打开目录
	f, err := os.Open(name)
	if err != nil {
		// 如果打开失败，认为目录不为空
		return false
	}
	// 确保关闭目录
	defer f.Close()

	// 尝试读取一个目录项
	_, err = f.Readdirnames(1)
	if err == io.EOF {
		// 如果读到文件末尾(EOF)，说明目录为空
		return true
	}
	// 其他情况认为目录不为空
	return false
}

// StartSSHServer 启动SSH服务器并处理传入连接
// 参数:
//
//	sshListener - 网络监听器
//	privateKey - SSH服务器私钥
//	insecure - 是否启用不安全模式
//	openproxy - 是否开放代理
//	dataDir - 数据目录路径
//	timeout - 连接超时时间
func StartSSHServer(sshListener net.Listener, privateKey ssh.Signer, insecure, openproxy bool, dataDir string, timeout int) {
	// 设置授权密钥文件路径
	adminAuthorizedKeysPath := filepath.Join(dataDir, "authorized_keys")
	authorizedControlleeKeysPath := filepath.Join(dataDir, "authorized_controllee_keys")
	authorizedProxyKeysPath := filepath.Join(dataDir, "authorized_proxy_keys")

	// 创建下载目录(如果不存在)
	downloadsDir := filepath.Join(dataDir, "downloads")
	if _, err := os.Stat(downloadsDir); err != nil && os.IsNotExist(err) {
		os.Mkdir(downloadsDir, 0700)
		log.Println("Created downloads directory (", downloadsDir, ")")
	}

	// 创建用户密钥目录(如果不存在)
	usersKeysDir := filepath.Join(dataDir, "keys")
	if _, err := os.Stat(usersKeysDir); err != nil && os.IsNotExist(err) {
		os.Mkdir(usersKeysDir, 0700)
		log.Println("Created user keys directory (", usersKeysDir, ")")
	}

	// 检查管理员密钥文件是否存在
	if _, err := os.Stat(adminAuthorizedKeysPath); err != nil && os.IsNotExist(err) && isDirEmpty(usersKeysDir) {
		log.Println("WARNING: authorized_keys file does not exist in server directory, and no user keys are registered. You will not be able to log in to this server!")
	}

	// 配置SSH服务器
	config := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-OpenSSH_8.0",
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			// 获取客户端IP地址
			remoteIp := getIP(conn.RemoteAddr().String())
			// 检查是否为不可信的转发连接
			isUntrustWorthy := conn.RemoteAddr().Network() == "remote_forward_tcp"

			if remoteIp == nil {
				return nil, fmt.Errorf("not authorized %q, could not parse IP address %s", conn.User(), conn.RemoteAddr())
			}

			// 首先检查管理员密钥
			perm, err := CheckAuth(adminAuthorizedKeysPath, key, remoteIp, false)
			if err == nil && !isUntrustWorthy {
				perm.Extensions["type"] = "user"
				perm.Extensions["privilege"] = "5"
				return perm, err
			}
			if err != ErrKeyNotInList {
				// 处理管理员登录失败
				err = fmt.Errorf("admin with supplied username (%s) denied login: %s", strconv.QuoteToGraphic(conn.User()), err)
				if isUntrustWorthy {
					err = fmt.Errorf("admin (%s) denied login: cannot connect admins via pivoted server port (may result in allow list bypass)", strconv.QuoteToGraphic(conn.User()))
				}
				return nil, err
			}

			// 检查普通用户密钥(防止路径遍历)
			authorisedKeysPath := filepath.Join(usersKeysDir, filepath.Join("/", filepath.Clean(conn.User())))
			perm, err = CheckAuth(authorisedKeysPath, key, remoteIp, false)
			if err == nil && !isUntrustWorthy {
				perm.Extensions["type"] = "user"
				perm.Extensions["privilege"] = "0"
				return perm, err
			}

			if err != ErrKeyNotInList {
				// 处理用户登录失败
				err = fmt.Errorf("user (%s) denied login: %s", strconv.QuoteToGraphic(conn.User()), err)
				if isUntrustWorthy {
					err = fmt.Errorf("user (%s) denied login: cannot connect users via pivoted server port (may result in allow list bypass)", strconv.QuoteToGraphic(conn.User()))
				}
				return nil, err
			}

			// 检查控制客户端密钥(不安全模式下允许任何客户端)
			perms, err := CheckAuth(authorizedControlleeKeysPath, key, remoteIp, insecure)
			if err == nil {
				perms.Extensions["type"] = "client"
				return perms, err
			}

			if err != ErrKeyNotInList {
				return nil, fmt.Errorf("client was denied login: %s", err)
			}

			// 检查代理密钥(不安全或开放代理模式下)
			perms, err = CheckAuth(authorizedProxyKeysPath, key, remoteIp, insecure || openproxy)
			if err == nil {
				perms.Extensions["type"] = "proxy"
				return perms, err
			}

			if err != ErrKeyNotInList {
				return nil, fmt.Errorf("proxy was denied login: %s", err)
			}

			return nil, fmt.Errorf("not authorized %q, potentially you might want to enable --insecure mode", conn.User())
		},
	}

	// 添加主机密钥
	config.AddHostKey(privateKey)

	// 注册连接状态观察者
	observers.ConnectionState.Register(func(c observers.ClientState) {
		var arrowDirection = "<-"
		if c.Status == "disconnected" {
			arrowDirection = "->"
		}

		// 记录连接状态到日志文件
		f, err := os.OpenFile(filepath.Join(dataDir, "watch.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			log.Println("unable to open watch log for writing:", err)
		}
		defer f.Close()

		if _, err := f.WriteString(fmt.Sprintf("%s %s %s (%s %s) %s %s\n",
			c.Timestamp.Format("2006/01/02 15:04:05"),
			arrowDirection,
			c.HostName,
			c.IP,
			c.ID,
			c.Version,
			c.Status)); err != nil {
			log.Println(err)
		}
	})

	// 主循环 - 接受所有连接
	for {
		conn, err := sshListener.Accept()
		if err != nil {
			log.Printf("Failed to accept incoming connection (%s)", err)
			continue
		}

		// 启动goroutine处理连接
		go acceptConn(conn, config, timeout, dataDir)
	}
}

// getIP 从可能包含端口号的字符串中提取IP地址
// 参数:
//
//	ip - 可能包含端口号或IPv6方括号的IP字符串
//
// 返回值:
//
//	net.IP - 解析后的IP地址，解析失败返回nil
func getIP(ip string) net.IP {
	// 从字符串末尾向前查找冒号(处理端口号)
	for i := len(ip) - 1; i > 0; i-- {
		if ip[i] == ':' {
			// 去除可能存在的IPv6方括号
			return net.ParseIP(strings.Trim(strings.Trim(ip[:i], "]"), "["))
		}
	}

	return nil
}

// acceptConn 处理传入的SSH连接并根据类型路由
// 参数:
//
//	c - 网络连接对象
//	config - SSH服务器配置
//	timeout - 连接超时时间(分钟)
//	dataDir - 数据存储目录
func acceptConn(c net.Conn, config *ssh.ServerConfig, timeout int, dataDir string) {
	// 设置初始高超时(允许用户输入SSH密钥密码)
	realConn := &internal.TimeoutConn{Conn: c, Timeout: time.Duration(timeout) * time.Minute}

	// 执行SSH握手
	sshConn, chans, reqs, err := ssh.NewServerConn(realConn, config)
	if err != nil {
		log.Printf("SSH握手失败 (%s)", err.Error())
		return
	}

	// 创建客户端专属日志
	clientLog := logger.NewLog(sshConn.RemoteAddr().String())

	if timeout > 0 {
		// 设置实际超时(默认5秒心跳，10秒超时)
		realConn.Timeout = time.Duration(timeout*2) * time.Second

		// 启动心跳检测goroutine
		go func() {
			for {
				_, _, err = sshConn.SendRequest("keepalive-rssh@golang.org", true, []byte(fmt.Sprintf("%d", timeout)))
				if err != nil {
					clientLog.Info("心跳检测失败，客户端已断开连接")
					sshConn.Close()
					return
				}
				time.Sleep(time.Duration(timeout) * time.Second)
			}
		}()
	}

	// 根据连接类型进行路由
	switch sshConn.Permissions.Extensions["type"] {
	case "user":
		// 处理用户(管理员/普通用户)连接
		user, connectionDetails, err := users.CreateOrGetUser(sshConn.User(), sshConn)
		if err != nil {
			sshConn.Close()
			log.Println(err)
			return
		}

		// 处理用户会话通道
		go func() {
			err = registerChannelCallbacks(connectionDetails, user, chans, clientLog, map[string]func(connectionDetails string, user *users.User, newChannel ssh.NewChannel, log logger.Logger){
				"session":      handlers.Session(dataDir), // shell会话
				"direct-tcpip": handlers.LocalForward,     // 本地端口转发
			})
			clientLog.Info("用户断开连接: %s", err.Error())

			users.DisconnectUser(sshConn)
		}()

		clientLog.Info("新用户SSH连接，版本 %s", sshConn.ClientVersion())

		// 丢弃全局请求(tcpip-forward除外)
		go ssh.DiscardRequests(reqs)

	case "client":
		// 处理可控客户端连接
		id, username, err := users.AssociateClient(sshConn)
		if err != nil {
			clientLog.Error("无法添加新客户端 %s", err)
			sshConn.Close()
			return
		}

		go func() {
			go ssh.DiscardRequests(reqs)

			// 注册客户端专属通道处理器
			err = registerChannelCallbacks("", nil, chans, clientLog, map[string]func(_ string, user *users.User, newChannel ssh.NewChannel, log logger.Logger){
				"rssh-download":   handlers.Download(dataDir),     // 文件下载
				"forwarded-tcpip": handlers.ServerPortForward(id), // 远程端口转发
			})

			clientLog.Info("SSH客户端已断开连接")
			users.DisassociateClient(id, sshConn)

			// 通知观察者连接断开
			observers.ConnectionState.Notify(observers.ClientState{
				Status:    "disconnected",
				ID:        id,
				IP:        sshConn.RemoteAddr().String(),
				HostName:  username,
				Version:   string(sshConn.ClientVersion()),
				Timestamp: time.Now(),
			})
		}()

		clientLog.Info("新的可控连接来自 %s，ID %s", color.BlueString(username), color.YellowString(id))

		// 通知观察者新连接
		observers.ConnectionState.Notify(observers.ClientState{
			Status:    "connected",
			ID:        id,
			IP:        sshConn.RemoteAddr().String(),
			HostName:  username,
			Version:   string(sshConn.ClientVersion()),
			Timestamp: time.Now(),
		})

	case "proxy":
		// 处理代理连接
		clientLog.Info("新的远程动态转发连接: %s", sshConn.ClientVersion())

		go internal.DiscardChannels(sshConn, chans)
		go handlers.RemoteDynamicForward(sshConn, reqs, clientLog)

	default:
		// 拒绝未知连接类型
		sshConn.Close()
		clientLog.Warning("客户端连接但类型未知，已终止: %s", sshConn.Permissions.Extensions["type"])
	}
}
