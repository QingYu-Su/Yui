package commands

import (
	"fmt"
	"io"
	"log"
	"sort"
	"strings"

	"github.com/QingYu-Su/Yui/internal/server/users"          // 用户管理模块
	"github.com/QingYu-Su/Yui/internal/terminal"              // 终端处理模块
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete" // 自动补全功能
	"github.com/QingYu-Su/Yui/pkg/table"                      // 表格输出工具
	"github.com/fatih/color"                                  // 终端颜色输出
	"golang.org/x/crypto/ssh"                                 // SSH协议库
)

// list 结构体定义了列出客户端连接的命令类型
type list struct {
}

// displayItem 结构体用于存储要显示的客户端连接信息
type displayItem struct {
	sc ssh.ServerConn // SSH服务器连接对象
	id string         // 客户端ID
}

// fancyTable 函数用于以美观的表格格式显示客户端连接信息
// 参数:
//   - tty: 终端输入输出接口
//   - applicable: 要显示的客户端连接信息切片
func fancyTable(tty io.ReadWriter, applicable []displayItem) {
	// 创建包含四列的表格: 目标(Targets)、ID(IDs)、所有者(Owners)、版本(Version)
	t, _ := table.NewTable("Targets", "IDs", "Owners", "Version")

	for _, a := range applicable {
		// 获取公钥指纹或注释作为keyId
		keyId := a.sc.Permissions.Extensions["pubkey-fp"]
		if a.sc.Permissions.Extensions["comment"] != "" {
			keyId = a.sc.Permissions.Extensions["comment"]
		}

		// 处理所有者信息
		owners := a.sc.Permissions.Extensions["owners"]
		if owners == "" {
			owners = "public" // 默认显示为public
		} else {
			// 将逗号分隔的所有者转换为多行显示
			owners = strings.Join(strings.Split(a.sc.Permissions.Extensions["owners"], ","), "\n")
		}

		// 添加一行数据到表格中
		if err := t.AddValues(
			// 第一列: 组合显示ID、keyId、用户名和远程地址
			fmt.Sprintf("%s\n%s\n%s\n%s\n",
				a.id,
				keyId,
				users.NormaliseHostname(a.sc.User()),
				a.sc.RemoteAddr().String()),
			owners,                       // 第二列: 所有者信息
			string(a.sc.ClientVersion()), // 第三列: 客户端版本
		); err != nil {
			log.Println("Error drawing pretty ls table (THIS IS A BUG): ", err)
			return
		}
	}

	// 输出表格到终端
	t.Fprint(tty)
}

// ValidArgs 方法返回 list 命令的有效参数及其描述
func (l *list) ValidArgs() map[string]string {
	return map[string]string{
		"t": "Print all attributes in pretty table", // t参数: 以美观表格格式显示
		"h": "Print help",                           // h参数: 显示帮助
	}
}

// Run 方法执行列出客户端连接的操作
func (l *list) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 处理过滤器参数
	filter := ""
	if len(line.ArgumentsAsStrings()) > 0 {
		// 如果有普通参数，合并为过滤器字符串
		filter = strings.Join(line.ArgumentsAsStrings(), " ")
	} else if len(line.FlagsOrdered) > 1 {
		// 处理标志后面的参数作为过滤器
		args := line.FlagsOrdered[len(line.FlagsOrdered)-1].Args
		if len(args) != 0 {
			filter = line.RawLine[args[0].End():]
		}
	}

	var toReturn []displayItem // 存储要显示的客户端信息

	// 根据过滤器查找匹配的客户端
	matchingClients, err := user.SearchClients(filter)
	if err != nil {
		return err
	}

	// 检查是否找到匹配的客户端
	if len(matchingClients) == 0 {
		if len(filter) == 0 {
			return fmt.Errorf("No RSSH clients connected") // 无过滤器且无客户端连接
		}
		return fmt.Errorf("Unable to find match for '" + filter + "'") // 有过滤器但无匹配
	}

	// 对客户端ID进行排序
	ids := []string{}
	for id := range matchingClients {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// 准备要显示的数据
	for _, id := range ids {
		toReturn = append(toReturn, displayItem{
			id: id,
			sc: *matchingClients[id],
		})
	}

	// 如果设置了-t参数，使用美观表格格式输出
	if line.IsSet("t") {
		fancyTable(tty, toReturn)
		return nil
	}

	// 默认格式输出
	sep := "\n"
	for i, tr := range toReturn {
		// 获取公钥指纹或注释
		keyId := tr.sc.Permissions.Extensions["pubkey-fp"]
		if tr.sc.Permissions.Extensions["comment"] != "" {
			keyId = tr.sc.Permissions.Extensions["comment"]
		}

		// 处理所有者信息
		owners := tr.sc.Permissions.Extensions["owners"]
		if owners == "" {
			owners = "public"
		}

		// 格式化输出每个客户端信息
		fmt.Fprintf(tty, "%s %s %s %s, owners: %s, version: %s",
			color.YellowString(tr.id), // 黄色显示ID
			keyId,
			color.BlueString(users.NormaliseHostname(tr.sc.User())), // 蓝色显示用户名
			tr.sc.RemoteAddr().String(),
			owners,
			tr.sc.ClientVersion())

		// 如果不是最后一项，添加分隔符
		if i != len(toReturn)-1 {
			fmt.Fprint(tty, sep)
		}
	}

	fmt.Fprint(tty, "\n") // 最后添加换行
	return nil
}

// Expect 方法返回自动补全的期望输入类型
func (l *list) Expect(line terminal.ParsedLine) []string {
	// 如果参数数量<=1，提供远程ID的自动补全
	if len(line.Arguments) <= 1 {
		return []string{autocomplete.RemoteId}
	}
	return nil
}

// Help 方法返回list命令的帮助信息
func (l *list) Help(explain bool) string {
	if explain {
		return "List connected controllable hosts." // 简要说明
	}

	// 完整帮助信息
	return terminal.MakeHelpText(
		l.ValidArgs(),          // 有效参数列表
		"ls [OPTION] [FILTER]", // 使用语法
		"Filter uses glob matching against all attributes of a target (id, public key hash, hostname, ip)", // 详细说明
	)
}
