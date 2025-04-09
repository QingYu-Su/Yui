package users

import (
	"errors"        // 用于定义错误类型
	"fmt"           // 格式化输入输出
	"log"           // 日志记录
	"path/filepath" // 用于路径操作
	"sort"          // 排序
	"strconv"       // 字符串与数字的转换
	"sync"          // 同步工具，用于并发控制

	"github.com/QingYu-Su/Yui/internal" // 内部包
	"github.com/QingYu-Su/Yui/pkg/trie" // 引入Trie树包
	"golang.org/x/crypto/ssh"           // SSH相关功能
)

// 常量定义用户权限等级
const (
	UserPermissions  = 0 // 普通用户权限
	AdminPermissions = 5 // 管理员权限
)

// 定义错误类型，表示服务器连接为空
var ErrNilServerConnection = errors.New("the server connection was nil for the client")

// 全局变量
var (
	lck sync.RWMutex // 读写锁，用于并发控制
	// 用户名到用户对象的映射
	users = map[string]*User{}
	// 当前活跃的连接
	activeConnections = map[string]bool{}
)

// Connection 表示用户与服务器的连接
type Connection struct {
	// 用户与服务器的SSH连接，用于创建新通道等操作，不应直接进行io.Copy操作
	serverConnection ssh.Conn

	// 终端请求对象
	Pty *internal.PtyReq

	// Shell请求通道
	ShellRequests <-chan *ssh.Request

	// 用于记录当前连接的详细信息
	ConnectionDetails string
}

// User 表示用户对象
type User struct {
	sync.RWMutex // 读写锁，用于并发控制

	// 用户的所有连接映射
	userConnections map[string]*Connection
	// 用户名
	username string

	// 用户的所有SSH客户端连接
	clients map[string]*ssh.ServerConn
	// 自动补全功能的Trie树
	autocomplete *trie.Trie

	// 用户的权限等级指针
	privilege *int
}

// SetOwnership 设置用户的连接所有权
func (u *User) SetOwnership(uniqueID, newOwners string) error {
	// 加锁，确保并发安全
	lck.Lock()
	defer lck.Unlock()

	// 从用户的客户端连接中查找指定的连接
	sc, ok := u.clients[uniqueID]
	if !ok {
		// 如果未找到，尝试从全局的共享连接中查找
		if sc, ok = ownedByAll[uniqueID]; !ok {
			// 如果用户是管理员，尝试从所有客户端中查找
			if u.Privilege() == AdminPermissions {
				if sc, ok = allClients[uniqueID]; !ok {
					// 如果仍未找到，返回错误
					return errors.New("not found")
				}
			}
		}
	}

	// 如果新的所有者为空，表示该连接将共享给所有人
	if newOwners == "" {
		// 如果已经共享给所有人，则无需操作
		if _, ok := ownedByAll[uniqueID]; ok {
			return nil
		}
	}

	// 从旧的所有者中移除该连接
	_disassociateFromOwners(uniqueID, sc.Permissions.Extensions["owners"])
	// 将该连接关联到新的所有者
	_associateToOwners(uniqueID, newOwners, sc)

	// 更新连接的权限扩展信息
	sc.Permissions.Extensions["owners"] = newOwners

	return nil
}

// SearchClients 搜索符合过滤条件的客户端连接（可以搜索ID、别名和地址）
func (u *User) SearchClients(filter string) (out map[string]*ssh.ServerConn, err error) {
	// 在过滤条件后添加通配符，以便进行模式匹配
	filter = filter + "*"

	// 验证过滤条件是否格式正确
	_, err = filepath.Match(filter, "")
	if err != nil {
		// 如果过滤条件格式不正确，返回错误
		return nil, fmt.Errorf("filter is not well formed")
	}

	// 初始化返回的客户端连接映射
	out = make(map[string]*ssh.ServerConn)

	// 加读锁，确保并发安全
	lck.RLock()
	defer lck.RUnlock()

	// 根据用户权限确定搜索的客户端范围
	searchClients := u.clients
	if u.Privilege() == AdminPermissions {
		// 如果是管理员权限，搜索所有客户端
		searchClients = allClients
	}

	// 遍历客户端连接
	for id, conn := range searchClients {
		// 如果过滤条件为空，直接添加到结果中
		if filter == "" {
			out[id] = conn
			continue
		}

		// 检查客户端ID或远程地址是否匹配过滤条件
		if _matches(filter, id, conn.RemoteAddr().String()) {
			out[id] = conn
			continue
		}
	}

	// 如果用户不是管理员，还需要搜索共享给所有人的客户端
	if u.Privilege() != AdminPermissions {
		for id, conn := range ownedByAll {
			// 如果过滤条件为空，直接添加到结果中
			if filter == "" {
				out[id] = conn
				continue
			}

			// 检查客户端ID或远程地址是否匹配过滤条件
			if _matches(filter, id, conn.RemoteAddr().String()) {
				out[id] = conn
				continue
			}
		}
	}

	// 返回搜索结果
	return
}

// _matches 检查客户端ID或远程地址是否匹配过滤条件
func _matches(filter, clientId, remoteAddr string) bool {
	// 检查客户端ID是否匹配过滤条件
	match, _ := filepath.Match(filter, clientId)
	if match {
		return true
	}

	// 检查客户端ID的别名是否匹配过滤条件
	for _, alias := range uniqueIdToAllAliases[clientId] {
		match, _ = filepath.Match(filter, alias)
		if match {
			return true
		}
	}

	// 检查远程地址是否匹配过滤条件
	match, _ = filepath.Match(filter, remoteAddr)
	return match
}

// Matches 检查指定的客户端ID或远程地址是否匹配过滤条件
func (u *User) Matches(filter, clientId, remoteAddr string) bool {
	// 加读锁，确保并发安全
	lck.RLock()
	defer lck.RUnlock()

	// 调用 _matches 函数进行匹配检查
	return _matches(filter, clientId, remoteAddr)
}

// GetClient 根据标识符获取客户端连接
func (u *User) GetClient(identifier string) (*ssh.ServerConn, error) {
	// 加读锁，确保并发安全
	lck.RLock()
	defer lck.RUnlock()

	// 首先尝试从用户的客户端连接中查找
	if m, ok := u.clients[identifier]; ok {
		return m, nil
	}

	// 如果未找到，尝试从共享给所有人的客户端中查找
	if m, ok := ownedByAll[identifier]; ok {
		return m, nil
	}

	// 如果标识符是一个别名，尝试查找对应的唯一ID
	matchingUniqueIDs, ok := aliases[identifier]
	if !ok {
		// 如果别名不存在，返回错误
		return nil, fmt.Errorf("%s not found", identifier)
	}

	// 如果别名对应唯一的ID
	if len(matchingUniqueIDs) == 1 {
		for k := range matchingUniqueIDs {
			// 在用户的客户端连接中查找
			if m, ok := u.clients[k]; ok {
				return m, nil
			}

			// 在共享给所有人的客户端连接中查找
			if m, ok := ownedByAll[k]; ok {
				return m, nil
			}

			// 如果用户是管理员，尝试从所有客户端连接中查找
			if u.Privilege() == AdminPermissions {
				if m, ok := allClients[k]; ok {
					return m, nil
				}
			}
		}
	}

	// 如果别名对应多个ID，需要进一步处理
	matches := 0
	matchingHosts := ""
	for k := range matchingUniqueIDs {
		matches++

		// 尝试从用户客户端、共享客户端或所有客户端中查找
		client, ok := u.clients[k]
		if !ok {
			client, ok = ownedByAll[k]
			if !ok {
				if u.Privilege() == AdminPermissions {
					client = allClients[k]
				}
			}
		}

		// 记录匹配的客户端信息
		matchingHosts += fmt.Sprintf("%s (%s %s)\n", k, client.User(), client.RemoteAddr().String())
	}

	// 去掉最后一个换行符
	if len(matchingHosts) > 0 {
		matchingHosts = matchingHosts[:len(matchingHosts)-1]
	}
	// 返回错误，提示匹配到多个连接
	return nil, fmt.Errorf("%d connections match alias '%s'\n%s", matches, identifier, matchingHosts)
}

// Autocomplete 获取用户的自动补全功能
func (u *User) Autocomplete() *trie.Trie {
	// 如果用户是管理员，返回全局自动补全Trie树
	if u.privilege != nil && *u.privilege == AdminPermissions {
		return globalAutoComplete
	}

	// 否则返回用户的自动补全Trie树
	return u.autocomplete
}

// Session 根据连接详情获取用户的会话连接
func (u *User) Session(connectionDetails string) (*Connection, error) {
	// 在用户的连接映射中查找
	if c, ok := u.userConnections[connectionDetails]; ok {
		return c, nil
	}

	// 如果未找到，返回错误
	return nil, errors.New("session not found")
}

// Username 返回用户的用户名
func (u *User) Username() string {
	return u.username
}

// Privilege 返回用户的权限等级
func (u *User) Privilege() int {
	// 如果权限指针为空，记录日志并返回默认权限（无权限）
	if u.privilege == nil {
		log.Println("was unable to get privs of", u.username, "defaulting to 0 (no priv)")
		return 0
	}
	// 返回用户的权限等级
	return *u.privilege
}

// PrivilegeString 返回用户权限的字符串表示
func (u *User) PrivilegeString() string {
	// 如果权限指针为空，返回默认权限字符串
	if u.privilege == nil {
		return "0 (default)"
	}

	// 根据权限等级返回对应的字符串
	switch *u.privilege {
	case AdminPermissions:
		return fmt.Sprintf("%d admin", AdminPermissions)
	case UserPermissions:
		return fmt.Sprintf("%d user", UserPermissions)
	default:
		return "0 (default)"
	}
}

// _getUser 获取用户对象（非线程安全，仅内部使用）
func _getUser(username string) (*User, error) {
	// 从用户映射中查找用户
	u, ok := users[username]
	if !ok {
		// 如果用户不存在，返回错误
		return nil, errors.New("not found")
	}
	// 返回用户对象
	return u, nil
}

// CreateOrGetUser 创建或获取用户对象（线程安全）
func CreateOrGetUser(username string, serverConnection *ssh.ServerConn) (us *User, connectionDetails string, err error) {
	// 加写锁，确保并发安全
	lck.Lock()
	defer lck.Unlock()

	// 调用内部函数完成创建或获取用户
	return _createOrGetUser(username, serverConnection)
}

// _createOrGetUser 创建或获取用户对象（非线程安全，仅内部使用）
func _createOrGetUser(username string, serverConnection *ssh.ServerConn) (us *User, connectionDetails string, err error) {
	// 从用户映射中查找用户
	u, ok := users[username]
	if !ok {
		// 如果用户不存在，创建新用户
		newUser := &User{
			username:        username,
			userConnections: map[string]*Connection{},
			autocomplete:    trie.NewTrie(),
			clients:         make(map[string]*ssh.ServerConn),
		}

		// 将新用户添加到用户映射中
		users[username] = newUser
		u = newUser
	}

	// 如果提供了服务器连接
	if serverConnection != nil {
		// 创建新的连接对象
		newConnection := &Connection{
			serverConnection:  serverConnection,
			ShellRequests:     make(<-chan *ssh.Request),
			ConnectionDetails: makeConnectionDetailsString(serverConnection),
		}

		// 尝试解析服务器连接的权限等级
		priv, err := strconv.Atoi(serverConnection.Permissions.Extensions["privilege"])
		if err != nil {
			log.Println("could not parse privileges: ", err)
		} else {
			// 设置用户的权限等级
			u.privilege = &priv
		}

		// 检查是否已存在相同的连接
		if _, ok := u.userConnections[newConnection.ConnectionDetails]; ok {
			return nil, "", fmt.Errorf("connection already exists for %s", newConnection.ConnectionDetails)
		}

		// 将新连接添加到用户的连接映射中
		u.userConnections[newConnection.ConnectionDetails] = newConnection
		// 标记连接为活跃
		activeConnections[newConnection.ConnectionDetails] = true

		// 返回用户对象和连接详情
		return u, newConnection.ConnectionDetails, nil
	}

	// 如果未提供服务器连接，仅返回用户对象
	return u, "", nil
}

// makeConnectionDetailsString 根据服务器连接生成连接详情字符串
func makeConnectionDetailsString(ServerConnection *ssh.ServerConn) string {
	// 返回格式化的字符串，包含用户名和远程地址
	return fmt.Sprintf("%s@%s", ServerConnection.User(), ServerConnection.RemoteAddr().String())
}

// ListUsers 列出所有用户的用户名
func ListUsers() (userList []string) {
	// 加读锁，确保并发安全
	lck.RLock()
	defer lck.RUnlock()

	// 遍历用户映射，收集所有用户名
	for user := range users {
		userList = append(userList, user)
	}

	// 对用户名列表进行排序
	sort.Strings(userList)
	return
}

// DisconnectUser 断开用户的连接
func DisconnectUser(ServerConnection *ssh.ServerConn) {
	// 如果服务器连接不为空
	if ServerConnection != nil {
		// 加写锁，确保并发安全
		lck.Lock()
		defer lck.Unlock()

		// 确保在函数结束时关闭服务器连接
		defer ServerConnection.Close()

		// 生成连接详情字符串
		details := makeConnectionDetailsString(ServerConnection)

		// 从用户映射中查找对应用户
		user, ok := users[ServerConnection.User()]
		if !ok {
			// 如果用户不存在，直接返回
			return
		}

		// 从用户的连接映射中删除该连接
		delete(user.userConnections, details)
		// 从活跃连接映射中删除该连接
		delete(activeConnections, details)

		// 如果用户没有其他客户端连接，从用户映射中删除该用户
		if len(user.clients) == 0 {
			delete(users, user.username)
		}
	}
}
