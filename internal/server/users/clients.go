package users

import (
	"regexp"  // 正则表达式库，用于字符串匹配和替换
	"strings" // 字符串操作库

	"github.com/QingYu-Su/Yui/internal" // 内部包
	"github.com/QingYu-Su/Yui/pkg/trie" // Trie树包，用于自动补全等功能

	"golang.org/x/crypto/ssh" // SSH相关功能
)

// 全局变量
var (
	// 所有客户端连接的映射
	allClients = map[string]*ssh.ServerConn{}

	// 被所有用户共享的客户端连接映射
	ownedByAll = map[string]*ssh.ServerConn{}

	// 唯一ID到所有别名的映射
	uniqueIdToAllAliases = map[string][]string{}

	// 别名到唯一ID的映射
	aliases = map[string]map[string]bool{}

	// 用户名正则表达式，用于规范化用户名
	// 匹配不是单词字符（字母、数字和下划线）且不是短横线（-）的任意字符。
	usernameRegex = regexp.MustCompile(`[^\w-]`)

	// 全局自动补全Trie树
	globalAutoComplete = trie.NewTrie()

	// 公共客户端自动补全Trie树
	PublicClientsAutoComplete = trie.NewTrie()
)

// NormaliseHostname 规范化主机名
func NormaliseHostname(hostname string) string {
	// 将主机名转换为小写
	hostname = strings.ToLower(hostname)

	// 使用正则表达式替换非单词字符和非短横线为点号
	hostname = usernameRegex.ReplaceAllString(hostname, ".")

	// 返回规范化的主机名
	return hostname
}

// AssociateClient 将客户端连接关联到用户，并生成唯一标识符
func AssociateClient(conn *ssh.ServerConn) (string, string, error) {
	// 加写锁，确保并发安全
	lck.Lock()
	defer lck.Unlock()

	// 生成一个随机的唯一标识符
	idString, err := internal.RandomString(20)
	if err != nil {
		// 如果生成随机字符串失败，返回错误
		return "", "", err
	}

	// 规范化用户名
	username := NormaliseHostname(conn.User())

	// 为该连接添加别名
	addAlias(idString, username)                                 //客户端用户名
	addAlias(idString, conn.RemoteAddr().String())               //客户端地址
	addAlias(idString, conn.Permissions.Extensions["pubkey-fp"]) //客户端的公钥指纹
	if conn.Permissions.Extensions["comment"] != "" {
		addAlias(idString, conn.Permissions.Extensions["comment"]) //客户端的注释
	}

	// 将连接添加到所有客户端映射中
	allClients[idString] = conn

	// 将相关信息添加到全局自动补全Trie树中
	globalAutoComplete.AddMultiple(idString, username, conn.RemoteAddr().String(), conn.Permissions.Extensions["pubkey-fp"])
	if conn.Permissions.Extensions["comment"] != "" {
		globalAutoComplete.Add(conn.Permissions.Extensions["comment"])
	}

	// 根据连接的owners属性，将连接关联到相应的用户或公共列表
	_associateToOwners(idString, conn.Permissions.Extensions["owners"], conn)

	// 返回生成的唯一标识符和规范化后的用户名
	return idString, username, nil
}

// _associateToOwners 根据owners属性将连接关联到用户或公共列表
func _associateToOwners(idString, owners string, conn *ssh.ServerConn) {
	// 规范化用户名
	username := NormaliseHostname(conn.User())

	// 如果owners为空，将连接添加到公共列表
	ownersParts := strings.Split(owners, ",")
	if len(ownersParts) == 1 && ownersParts[0] == "" {
		ownedByAll[idString] = conn

		// 将相关信息添加到公共客户端自动补全Trie树中
		PublicClientsAutoComplete.AddMultiple(idString, username, conn.RemoteAddr().String(), conn.Permissions.Extensions["pubkey-fp"])
		if conn.Permissions.Extensions["comment"] != "" {
			PublicClientsAutoComplete.Add(conn.Permissions.Extensions["comment"])
		}
	} else {
		// 如果owners不为空，将连接关联到指定的用户
		for _, owner := range ownersParts {
			// 创建或获取用户对象（忽略错误，因为这里不添加连接）
			u, _, _ := _createOrGetUser(owner, nil)
			u.clients[idString] = conn

			// 将相关信息添加到用户的自动补全Trie树中
			u.autocomplete.AddMultiple(idString, username, conn.RemoteAddr().String(), conn.Permissions.Extensions["pubkey-fp"])
			if conn.Permissions.Extensions["comment"] != "" {
				u.autocomplete.Add(conn.Permissions.Extensions["comment"])
			}
		}
	}
}

// addAlias 为唯一ID添加别名
func addAlias(uniqueId, newAlias string) {
	// 如果别名映射中不存在该别名，初始化一个空映射
	if _, ok := aliases[newAlias]; !ok {
		aliases[newAlias] = make(map[string]bool)
	}

	// 将别名添加到唯一ID的别名列表中
	uniqueIdToAllAliases[uniqueId] = append(uniqueIdToAllAliases[uniqueId], newAlias)
	// 在别名映射中记录唯一ID
	aliases[newAlias][uniqueId] = true
}

// DisassociateClient 从系统中移除客户端连接
func DisassociateClient(uniqueId string, conn *ssh.ServerConn) {
	// 加写锁，确保并发安全
	lck.Lock()
	defer lck.Unlock()

	// 检查该唯一ID是否已存在于所有客户端映射中
	if _, ok := allClients[uniqueId]; !ok {
		// 如果已经移除，则无需再次移除
		return
	}

	// 从全局自动补全Trie树中移除唯一ID
	globalAutoComplete.Remove(uniqueId)

	// 获取该唯一ID的所有别名
	currentAliases, ok := uniqueIdToAllAliases[uniqueId]
	if ok {
		// 移除全局引用的别名和自动补全
		for _, alias := range currentAliases {
			// 如果该别名只对应一个唯一ID，则从全局自动补全Trie树中移除别名
			if len(aliases[alias]) <= 1 {
				globalAutoComplete.Remove(alias)
				delete(aliases, alias)
			}

			// 从别名映射中移除唯一ID
			delete(aliases[alias], uniqueId)
		}
	}

	// 从所有者映射中移除该唯一ID
	_disassociateFromOwners(uniqueId, conn.Permissions.Extensions["owners"])

	// 从所有客户端映射中移除该唯一ID
	delete(allClients, uniqueId)
	// 从唯一ID到别名的映射中移除该唯一ID
	delete(uniqueIdToAllAliases, uniqueId)
}

// _disassociateFromOwners 从所有者映射中移除唯一ID
func _disassociateFromOwners(uniqueId, owners string) {
	// 解析所有者字符串为所有者列表
	ownersParts := strings.Split(owners, ",")

	// 获取该唯一ID的所有别名
	currentAliases := uniqueIdToAllAliases[uniqueId]

	// 如果所有者为空，表示该客户端连接属于公共连接
	if len(ownersParts) == 1 && ownersParts[0] == "" {
		// 从公共客户端映射中移除该唯一ID
		delete(ownedByAll, uniqueId)

		// 从公共客户端自动补全Trie树中移除唯一ID和相关别名
		PublicClientsAutoComplete.Remove(uniqueId)
		PublicClientsAutoComplete.RemoveMultiple(currentAliases...)

	} else {
		// 遍历所有者列表
		for _, owner := range ownersParts {
			// 获取所有者用户对象
			u, err := _getUser(owner)
			if err != nil {
				// 如果用户不存在，跳过
				continue
			}

			// 从用户客户端映射中移除该唯一ID
			delete(u.clients, uniqueId)

			// 从用户自动补全Trie树中移除唯一ID和相关别名
			u.autocomplete.Remove(uniqueId)
			u.autocomplete.RemoveMultiple(currentAliases...)

			// 如果用户没有其他客户端连接，则从用户映射中移除该用户
			if len(u.clients) == 0 {
				delete(users, owner)
			}
		}
	}
}
