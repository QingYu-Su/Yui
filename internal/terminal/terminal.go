// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/QingYu-Su/Yui/internal"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/internal/terminal/autocomplete"
	"github.com/QingYu-Su/Yui/pkg/trie"
)

// 定义终端相关错误
var (
	ErrCtrlD = errors.New("ctrl + D") // Ctrl+D 组合键错误
)

// TerminalFunctionCallback 定义终端命令回调函数类型
type TerminalFunctionCallback func(term *Terminal, args ...string) error

// EscapeCodes 包含用于终端文本样式控制的转义序列
type EscapeCodes struct {
	// 前景色
	Black, Red, Green, Yellow, Blue, Magenta, Cyan, White []byte

	// 重置所有属性
	Reset []byte
}

// vt100EscapeCodes 定义VT100终端的颜色转义序列
var vt100EscapeCodes = EscapeCodes{
	Black:   []byte{keyEscape, '[', '3', '0', 'm'}, // 黑色
	Red:     []byte{keyEscape, '[', '3', '1', 'm'}, // 红色
	Green:   []byte{keyEscape, '[', '3', '2', 'm'}, // 绿色
	Yellow:  []byte{keyEscape, '[', '3', '3', 'm'}, // 黄色
	Blue:    []byte{keyEscape, '[', '3', '4', 'm'}, // 蓝色
	Magenta: []byte{keyEscape, '[', '3', '5', 'm'}, // 品红
	Cyan:    []byte{keyEscape, '[', '3', '6', 'm'}, // 青色
	White:   []byte{keyEscape, '[', '3', '7', 'm'}, // 白色

	Reset: []byte{keyEscape, '[', '0', 'm'}, // 重置样式
}

// Terminal 表示一个支持读取输入行的VT100终端状态
type Terminal struct {
	session *users.Connection // SSH客户端会话连接
	user    *users.User       // 当前用户
	cancel  chan bool         // 取消通道

	// 自动补全回调函数，每次按键时调用
	// 参数：终端实例、当前输入行、光标位置(字节索引)、按键rune
	// 返回：新输入行、新光标位置、是否处理完成
	AutoCompleteCallback func(term *Terminal, line string, pos int, key rune) (newLine string, newPos int, ok bool)

	Escape *EscapeCodes // 终端转义序列，始终有效但可能为空

	lock sync.Mutex // 保护终端状态和按键处理的互斥锁

	c      io.ReadWriter // 底层读写接口
	prompt []rune        // 终端提示符

	// 当前输入行
	line []rune
	// 光标在行中的逻辑位置
	pos int
	// 是否启用本地回显
	echo bool
	// 是否正在进行括号粘贴操作
	pasteActive bool

	// 光标位置信息
	cursorX int // 光标X坐标(0为左边界)
	cursorY int // 光标Y坐标(0为当前首行)
	// 记录的最大行数
	maxLine int

	// 终端尺寸
	termWidth, termHeight int

	// 待发送的终端数据
	outBuf []byte
	// 读取后剩余的部分按键序列(引用inBuf)
	remainder []byte
	inBuf     [256]byte // 输入缓冲区

	// 命令历史记录
	history stRingBuffer
	// 当前访问的历史记录索引(0表示最近一条)
	historyIndex int
	// 导航历史时可能返回未完成的初始行
	historyPending string

	// 自动补全索引项（当有多个补全匹配项时有效）以及自动补全光标伪装
	autoCompleteIndex, autoCompletePos int

	// 当前命令行字符串
	autoCompletePendng string

	// 是否开启自动补全状态
	autoCompleting bool

	// 注册的命令函数
	functions map[string]Command

	// 命令自动补全Trie树
	functionsAutoComplete *trie.Trie

	// 值自动补全Trie树
	// 每个值有多个Trie树，主要是为了解决远程和本地之间的共同补全，在自动补全时会汇集到一起并给到用户提示
	// 这里的key都为<...>格式
	autoCompleteValues map[string][]*trie.Trie

	// 是否为原始模式
	raw bool

	// 原始模式溢出通道
	rawOverflow chan []byte
}

// EnableRaw 启用终端原始模式
// 在原始模式下，输入字符不会被处理而是直接传递
func (t *Terminal) EnableRaw() {
	t.lock.Lock()
	defer t.lock.Unlock()

	if !t.raw {
		t.cancel <- true // 发送取消信号，取消窗口大小自动调整
		t.raw = true     // 设置原始模式标志
	}
}

// DisableRaw 禁用终端原始模式
// 退出原始模式并重新处理窗口大小变化
func (t *Terminal) DisableRaw() {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.raw {
		t.rawOverflow = make(chan []byte, 1) // 创建原始模式溢出通道

		t.raw = false // 清除原始模式标志

		t.handleWindowSize() // 处理窗口大小变化，设置窗口大小自动调整
	}
}

// NewTerminal 创建一个新的VT100终端实例
// 参数:
//   - c: 底层读写接口，如果是本地终端需要先设置为原始模式
//   - prompt: 提示符字符串，显示在每行输入前(如"> ")
//
// 返回:
//   - *Terminal: 新建的终端实例
func NewTerminal(c io.ReadWriter, prompt string) *Terminal {
	return &Terminal{
		Escape:       &vt100EscapeCodes, // 使用VT100转义序列
		c:            c,                 // 设置读写接口
		prompt:       []rune(prompt),    // 转换提示符为rune切片
		termWidth:    80,                // 默认终端宽度
		termHeight:   24,                // 默认终端高度
		echo:         true,              // 启用回显
		historyIndex: -1,                // 初始化历史记录索引
	}
}

// NewAdvancedTerminal 创建一个高级终端实例
// 相比NewTerminal增加了用户会话、自动补全等功能支持
// 参数:
//   - c: 底层读写接口
//   - user: 关联的用户对象
//   - session: 用户会话连接
//   - prompt: 提示符字符串
//
// 返回:
//   - *Terminal: 新建的高级终端实例
func NewAdvancedTerminal(c io.ReadWriter, user *users.User, session *users.Connection, prompt string) *Terminal {
	t := &Terminal{
		session:               session,                       // 用户会话连接
		user:                  user,                          // 关联用户
		cancel:                make(chan bool),               // 创建取消通道
		Escape:                &vt100EscapeCodes,             // 使用VT100转义序列
		c:                     c,                             // 设置读写接口
		prompt:                []rune(prompt),                // 转换提示符为rune切片
		termWidth:             80,                            // 默认终端宽度
		termHeight:            24,                            // 默认终端高度
		echo:                  true,                          // 启用回显
		historyIndex:          -1,                            // 初始化历史记录索引
		AutoCompleteCallback:  defaultAutoComplete,           // 设置默认自动补全回调
		functionsAutoComplete: trie.NewTrie(),                // 创建命令自动补全Trie树
		functions:             make(map[string]Command),      // 初始化命令映射
		autoCompleteValues:    make(map[string][]*trie.Trie), // 初始化自动补全值缓存
	}

	// 添加命令自动补全树到<functions>中
	t.AddValueAutoComplete(autocomplete.Functions, t.functionsAutoComplete)

	// 处理初始窗口大小
	t.handleWindowSize()

	return t
}

// handleWindowSize 处理终端窗口大小变化
// 启动一个goroutine监听窗口大小变化请求，并实时调整终端尺寸
func (t *Terminal) handleWindowSize() {
	go func() {
		for {
			select {
			case <-t.cancel: // 收到取消信号，退出goroutine
				return
			case req := <-t.session.ShellRequests: // 处理shell请求
				if req == nil { // 通道已关闭，结束处理
					return
				}

				switch req.Type { // 根据请求类型处理
				case "window-change": // 窗口大小变化请求
					// 解析新的宽度和高度
					w, h := internal.ParseDims(req.Payload)
					// 设置终端新尺寸
					t.SetSize(int(w), int(h))

					// 更新会话的PTY尺寸
					t.session.Pty.Columns = w
					t.session.Pty.Rows = h

				default: // 未知请求类型
					log.Println("在默认处理器中处理了未知请求类型: ", req.Type)
					if req.WantReply { // 如果需要回复
						req.Reply(false, nil) // 回复失败
					}
				}
			}
		}
	}()
}

// GetWidth 获取终端当前宽度
// 返回:
//   - int: 终端宽度(字符数)
func (t *Terminal) GetWidth() int {
	return int(t.termWidth)
}

// AddValueAutoComplete 添加自动补全值到指定位置
// 参数:
//   - placement: 自动补全值的位置标识
//   - trie: 要添加的Trie树(可变参数，可多个)
//
// 返回:
//   - error: 如果该位置已有值则返回错误
func (t *Terminal) AddValueAutoComplete(placement string, trie ...*trie.Trie) error {
	t.lock.Lock() // 加锁保证线程安全
	defer t.lock.Unlock()

	// 检查该位置是否已有自动补全值
	if _, ok := t.autoCompleteValues[placement]; ok {
		return errors.New("该位置的自动补全值已存在，忽略本次添加")
	}

	// 添加新的自动补全Trie树
	t.autoCompleteValues[placement] = trie

	return nil
}

// defaultAutoComplete 默认的自动补全处理函数
// 参数:
//   - term: 终端实例
//   - line: 当前输入行
//   - pos: 当前光标位置
//   - key: 按键rune值
//
// 返回值:
//   - newLine: 补全后的新行
//   - newPos: 新光标位置
//   - ok: 是否处理成功
func defaultAutoComplete(term *Terminal, line string, pos int, key rune) (newLine string, newPos int, ok bool) {
	// 仅处理Tab键
	if key == '\t' {
		// 当首次按下Tab时，初始化补全状态，保存当前输入内容和光标位置
		if !term.autoCompleting {
			term.startAutoComplete(line, pos)
		}

		// 解析当前输入行
		parsedLine := ParseLine(term.autoCompletePendng, term.autoCompletePos)

		var matches []string
		// 如果没有输入命令，则匹配所有可用命令
		// 示例：直接按Tab会显示所有可用命令
		if parsedLine.Command == nil {
			matches = term.functionsAutoComplete.PrefixMatch("")
		} else {
			// 如果焦点在命令部分，匹配命令前缀
			//示例：输入hel补全为help
			if parsedLine.Focus != nil && parsedLine.Focus.Start() == 0 {
				matches = term.functionsAutoComplete.PrefixMatch(parsedLine.Focus.Value())
			} else {
				// 查找已注册的命令函数
				if function, ok := term.functions[parsedLine.Command.Value()]; ok {
					// 获取命令期望的参数类型
					expected := function.Expect(parsedLine)

					if expected != nil {
						matches = expected

						// 处理特殊自动补全标记(如<file>)
						if len(expected) == 1 && len(expected[0]) > 1 {
							//检查是否为<...>格式的标记，这类标记是内置的特殊标记，需要特殊处理
							if expected[0][0] == '<' && expected[0][len(expected[0])-1] == '>' {
								// 查找预定义的自动补全值
								if trie, ok := term.autoCompleteValues[expected[0]]; ok {
									searchString := ""
									// 如果当前有聚焦节点，且要么没有所属标志，要么所属标志不为空
									if parsedLine.Focus != nil && (parsedLine.Section == nil || parsedLine.Focus.Start() != parsedLine.Section.Start()) {
										// 获取当前焦点值作为搜索前缀
										searchString = parsedLine.Focus.Value()
									}

									// 从Trie树中获取匹配项
									matches = []string{}
									for _, t := range trie {
										matches = append(matches, t.PrefixMatch(searchString)...)
									}
								}
							}
						}
					}
				}
			}
		}

		// 对匹配结果排序
		sort.Strings(matches)

		// 重新解析原始输入行
		parsedLine = ParseLine(line, pos)

		// 只有一个匹配项时直接补全
		if len(matches) == 1 {
			term.resetAutoComplete()

			// 构建补全后的行
			output, newPos := buildDisplayLine(parsedLine.Focus, line, matches[0], pos)
			// 如果是命令补全，自动添加空格
			if parsedLine.Focus != nil && parsedLine.Focus.Type() == (Cmd{}.Type()) {
				output += " "
				newPos += 1
			}

			return output, newPos, true
		}

		// 多个匹配项时循环选择
		if len(matches) > 1 {
			currentMatch := matches[term.autoCompleteIndex]
			term.autoCompleteIndex = (term.autoCompleteIndex + 1) % len(matches)

			output, newPos := buildDisplayLine(parsedLine.Focus, line, currentMatch, pos)
			return output, newPos, true
		}
	} else {
		// 非Tab键重置自动补全状态
		term.resetAutoComplete()
	}

	return "", 0, false
}

// buildDisplayLine 构建补全后的显示行
// 参数:
//   - focus: 当前焦点节点
//   - line: 原始输入行
//   - match: 匹配的补全字符串
//   - currentPos: 当前光标位置
//
// 返回值:
//   - output: 构建后的行
//   - newPos: 新光标位置
func buildDisplayLine(focus Node, line string, match string, currentPos int) (output string, newPos int) {
	// 无焦点节点时的简单处理
	if focus == nil {
		output = line[:currentPos] + match
		newPos = len(output)
		output += line[currentPos:]
		return output, newPos
	}

	// 根据焦点节点类型处理
	switch focus.Type() {
	case Cmd{}.Type(): // 命令类型
	case Argument{}.Type(): // 参数类型
		output = line[:focus.Start()]
	case Flag{}.Type(): // 标志类型
		output = line[:focus.End()] + " "
	default:
		panic("未知节点类型: " + focus.Type())
	}

	// 拼接匹配字符串并计算新位置
	output += match
	newPos = len(output)
	output += line[focus.End():]

	return
}

// 定义终端控制键常量
const (
	keyCtrlC       = 3    // Ctrl+C (终止信号)
	keyCtrlD       = 4    // Ctrl+D (EOF/退出)
	keyCtrlU       = 21   // Ctrl+U (删除行首到光标处)
	keyEnter       = '\r' // 回车键
	keyEscape      = 27   // ESC键
	keyBackspace   = 127  // 退格键
	keyUnknown     = 0xd800 /* UTF-16代理区起始值 以下为自增枚举值 */ + iota
	keyUp          // 上箭头键
	keyDown        // 下箭头键
	keyLeft        // 左箭头键
	keyRight       // 右箭头键
	keyAltLeft     // Alt+左箭头(单词左移)
	keyAltRight    // Alt+右箭头(单词右移)
	keyHome        // Home键(行首)
	keyDel         // Delete键(删除后字符)
	keyEnd         // End键(行尾)
	keyDeleteWord  // 删除单词(Alt+Backspace)
	keyDeleteLine  // 删除整行(Ctrl+K)
	keyClearScreen // 清屏(Ctrl+L)
	keyPasteStart  // 粘贴开始标记
	keyPasteEnd    // 粘贴结束标记
)

// 定义常用控制序列
var (
	crlf       = []byte{'\r', '\n'}                         // 回车换行序列(Windows风格)
	pasteStart = []byte{keyEscape, '[', '2', '0', '0', '~'} // 粘贴开始序列
	pasteEnd   = []byte{keyEscape, '[', '2', '0', '1', '~'} // 粘贴结束序列
)

// bytesToKey 尝试从字节序列解析按键，返回解析到的键值和剩余字节
// 参数:
//   - b: 输入字节序列
//   - pasteActive: 是否处于粘贴模式
//
// 返回值:
//   - rune: 解析到的键值(解析失败返回utf8.RuneError)
//   - []byte: 剩余未解析的字节
func bytesToKey(b []byte, pasteActive bool) (rune, []byte) {
	if len(b) == 0 {
		return utf8.RuneError, nil
	}

	// 非粘贴模式下处理控制键
	if !pasteActive {
		switch b[0] {
		case 1: // ^A (Home键)
			return keyHome, b[1:]
		case 2: // ^B (左箭头)
			return keyLeft, b[1:]
		case 5: // ^E (End键)
			return keyEnd, b[1:]
		case 6: // ^F (右箭头)
			return keyRight, b[1:]
		case 8: // ^H (退格键)
			return keyBackspace, b[1:]
		case 11: // ^K (删除行)
			return keyDeleteLine, b[1:]
		case 12: // ^L (清屏)
			return keyClearScreen, b[1:]
		case 23: // ^W (删除单词)
			return keyDeleteWord, b[1:]
		case 14: // ^N (下箭头)
			return keyDown, b[1:]
		case 16: // ^P (上箭头)
			return keyUp, b[1:]
		}
	}

	// 处理非转义字符
	if b[0] != keyEscape {
		if !utf8.FullRune(b) { // 检查是否完整UTF-8字符
			return utf8.RuneError, b
		}
		r, l := utf8.DecodeRune(b) // 解码UTF-8字符
		return r, b[l:]
	}

	// 处理转义序列(ESC [开头)
	if !pasteActive && len(b) >= 3 && b[0] == keyEscape && b[1] == '[' {
		switch b[2] {
		case 'A': // 上箭头
			return keyUp, b[3:]
		case 'B': // 下箭头
			return keyDown, b[3:]
		case 'C': // 右箭头
			return keyRight, b[3:]
		case 'D': // 左箭头
			return keyLeft, b[3:]
		case 'H': // Home键
			return keyHome, b[3:]
		case 'F': // End键
			return keyEnd, b[3:]
		case 51: // Delete键(ESC [3~)
			return keyDel, b[4:]
		}
	}

	// 处理Alt+方向键组合(ESC [1;3开头)
	if !pasteActive && len(b) >= 6 && b[0] == keyEscape && b[1] == '[' && b[2] == '1' && b[3] == ';' && b[4] == '3' {
		switch b[5] {
		case 'C': // Alt+右箭头
			return keyAltRight, b[6:]
		case 'D': // Alt+左箭头
			return keyAltLeft, b[6:]
		}
	}

	// 处理粘贴开始标记
	if !pasteActive && len(b) >= 6 && bytes.Equal(b[:6], pasteStart) {
		return keyPasteStart, b[6:]
	}

	// 处理粘贴结束标记
	if pasteActive && len(b) >= 6 && bytes.Equal(b[:6], pasteEnd) {
		return keyPasteEnd, b[6:]
	}

	// 处理未知序列: 查找序列结束标记([a-zA-Z~])
	for i, c := range b[0:] {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '~' {
			return keyUnknown, b[i+1:]
		}
	}

	// 无法识别的序列
	return utf8.RuneError, b
}

// AddCommands 添加命令集合到终端
// 参数:
//   - m: 命令名称到Command实现的映射
//
// 返回值:
//   - error: 总是返回nil(保留错误处理能力)
func (t *Terminal) AddCommands(m map[string]Command) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	// 更新命令集合
	t.functions = m

	// 将命令名称添加到自动补全Trie树
	for k := range t.functions {
		t.functionsAutoComplete.Add(k)
	}

	return nil
}

// removeDuplicates 移除字符串切片中的重复项并排序
// 参数:
//   - stringsSlice: 待处理的字符串切片
//
// 返回值:
//   - []string: 去重排序后的切片
func (t *Terminal) removeDuplicates(stringsSlice []string) []string {
	allKeys := make(map[string]bool) // 用于去重的map
	list := []string{}

	// 遍历切片去重
	for _, item := range stringsSlice {
		if _, value := allKeys[item]; !value {
			allKeys[item] = true
			list = append(list, item)
		}
	}

	sort.Strings(list) // 排序结果
	return list
}

// Run 启动终端主循环
// 返回值:
//   - error: 读取输入或执行命令时发生的错误
func (t *Terminal) Run() error {
	for {
		// 读取用户输入行
		line, err := t.ReadLine()
		if err != nil {
			return err
		}

		// 解析输入行
		parsedLine := ParseLine(line, t.pos)

		// 处理有效命令
		if parsedLine.Command != nil {
			// 查找命令实现
			f, ok := t.functions[parsedLine.Command.Value()]
			if !ok {
				fmt.Fprintf(t, "未知命令: %s\n", parsedLine.Command.Value())
				continue
			}

			// 检查帮助标志
			_, isSmallHelp := parsedLine.Flags["h"]
			_, isBigHelp := parsedLine.Flags["help"]

			// 显示帮助信息
			if isSmallHelp || isBigHelp {
				fmt.Fprint(t, f.Help(false))
				continue
			}

			// 验证标志参数
			validFlags := f.ValidArgs()
			failed := []string{}
			for flag := range parsedLine.Flags {
				_, ok := validFlags[flag]
				if !ok && !(flag == "h" || flag == "help") {
					failed = append(failed, flag)
				}
			}

			// 处理无效标志
			if len(failed) > 0 {
				failed = t.removeDuplicates(failed)
				suffix := ""
				if len(failed) > 1 {
					suffix = "s" // 复数形式
				}

				fmt.Fprintf(t, "无效标志%s: %q\n\n", suffix, strings.Join(failed, ", "))
				fmt.Fprint(t, f.Help(false))
				continue
			}

			// 执行命令
			err = f.Run(t.user, t, parsedLine)
			if err != nil {
				if err == io.EOF { // 处理终止信号
					return err
				}

				// 输出错误信息
				fmt.Fprintf(t, "%s\n", err)
			}
		}
	}
}

// queue 将数据追加到输出缓冲区末尾
// 参数:
//   - data: 要追加的rune切片
func (t *Terminal) queue(data []rune) {
	t.outBuf = append(t.outBuf, []byte(string(data))...)
}

// 空格字符常量
var space = []rune{' '}

// isPrintable 判断字符是否可打印
// 参数:
//   - key: 要检查的rune字符
//
// 返回值:
//   - bool: 是否可打印(排除控制字符和代理区字符)
func isPrintable(key rune) bool {
	isInSurrogateArea := key >= 0xd800 && key <= 0xdbff // UTF-16代理区检查
	return key >= 32 && !isInSurrogateArea              // 32以上且不在代理区
}

// moveCursorToPos 移动光标到指定逻辑位置
// 参数:
//   - pos: 目标位置(相对于输入起始位置)
func (t *Terminal) moveCursorToPos(pos int) {
	if !t.echo { // 无回显模式下不处理光标移动
		return
	}

	// 计算目标位置的x,y坐标
	x := visualLength(t.prompt) + pos // 总视觉长度=提示符+位置
	y := x / t.termWidth              // 计算行数
	x = x % t.termWidth               // 计算列数

	// 计算需要移动的方向和距离
	up := 0
	if y < t.cursorY {
		up = t.cursorY - y // 需要上移的行数
	}

	down := 0
	if y > t.cursorY {
		down = y - t.cursorY // 需要下移的行数
	}

	left := 0
	if x < t.cursorX {
		left = t.cursorX - x // 需要左移的列数
	}

	right := 0
	if x > t.cursorX {
		right = x - t.cursorX // 需要右移的列数
	}

	// 更新光标位置并执行移动
	t.cursorX = x
	t.cursorY = y
	t.move(up, down, left, right)
}

// move 生成光标移动控制序列并加入输出队列
// 参数:
//   - up: 上移行数
//   - down: 下移行数
//   - left: 左移列数
//   - right: 右移列数
func (t *Terminal) move(up, down, left, right int) {
	m := []rune{} // 存储生成的转义序列

	// 生成上移序列(ESC [nA)
	if up == 1 {
		m = append(m, keyEscape, '[', 'A') // 单行上移简写
	} else if up > 1 {
		m = append(m, keyEscape, '[')
		m = append(m, []rune(strconv.Itoa(up))...) // 多行上移
		m = append(m, 'A')
	}

	// 生成下移序列(ESC [nB)
	if down == 1 {
		m = append(m, keyEscape, '[', 'B') // 单行下移简写
	} else if down > 1 {
		m = append(m, keyEscape, '[')
		m = append(m, []rune(strconv.Itoa(down))...) // 多行下移
		m = append(m, 'B')
	}

	// 生成右移序列(ESC [nC)
	if right == 1 {
		m = append(m, keyEscape, '[', 'C') // 单列右移简写
	} else if right > 1 {
		m = append(m, keyEscape, '[')
		m = append(m, []rune(strconv.Itoa(right))...) // 多列右移
		m = append(m, 'C')
	}

	// 生成左移序列(ESC [nD)
	if left == 1 {
		m = append(m, keyEscape, '[', 'D') // 单列左移简写
	} else if left > 1 {
		m = append(m, keyEscape, '[')
		m = append(m, []rune(strconv.Itoa(left))...) // 多列左移
		m = append(m, 'D')
	}

	t.queue(m) // 将生成的序列加入输出队列
}

// Read 从终端读取数据到指定缓冲区
// 注意：此方法存在故意的竞态条件，由于底层阻塞读取的特性以及无法在不关闭连接的情况下取消读取
func (t *Terminal) Read(b []byte) (n int, err error) {
	// 原始模式下的特殊处理
	if t.raw {
		n, err := t.c.Read(b)

		// 处理原始模式切换时的数据溢出
		if !t.raw && t.rawOverflow != nil {
			c := make([]byte, n)
			copy(c, b[:n])
			t.rawOverflow <- c
		}

		return n, err
	}

	// 非原始模式下返回EOF
	return 0, io.EOF
}

// clearLineToRight 清除从光标位置到行尾的内容
func (t *Terminal) clearLineToRight() {
	op := []rune{keyEscape, '[', 'K'} // VT100清除行尾序列
	t.queue(op)
}

const maxLineLength = 4096 // 单行最大长度限制

// setLine 设置当前输入行内容并更新光标位置
// 参数:
//   - newLine: 新的行内容
//   - newPos: 新的光标位置
func (t *Terminal) setLine(newLine []rune, newPos int) {
	if t.echo {
		// 移动光标到行首并重写整行
		t.moveCursorToPos(0)
		t.writeLine(newLine)

		// 清除原有行多余内容
		for i := len(newLine); i < len(t.line); i++ {
			t.writeLine(space)
		}

		// 移动光标到新位置
		t.moveCursorToPos(newPos)
	}

	// 更新行内容和光标位置
	t.line = newLine
	t.pos = newPos
}

// addCharacterToInput 添加字符到输入缓冲区
// 注意：此方法线程安全，使用互斥锁保护
func (t *Terminal) addCharacterToInput(characters []byte) {
	t.lock.Lock()
	defer t.lock.Unlock()

	log.Println("添加输入字符: ", characters)

	// 拷贝字符到输入缓冲区，考虑缓冲区大小限制
	n := copy(t.inBuf[:], characters[:min(len(characters)-1):256])
	t.remainder = t.inBuf[:n]
}

// advanceCursor 按指定距离移动光标位置
// 参数:
//   - places: 移动的距离(正数表示前进，负数表示后退)
func (t *Terminal) advanceCursor(places int) {
	// 更新光标X坐标和行号
	t.cursorX += places
	t.cursorY += t.cursorX / t.termWidth

	// 更新最大行号记录
	if t.cursorY > t.maxLine {
		t.maxLine = t.cursorY
	}

	// 计算新的X坐标(考虑终端宽度)
	t.cursorX = t.cursorX % t.termWidth

	// 处理行尾换行特殊情况
	if places > 0 && t.cursorX == 0 {
		/*
		   通常终端在写入字符时会自动前进光标位置，
		   但行末字符除外。然而，当写入导致换行的
		   字符(换行符除外)时，光标位置会前进两格。

		   因此，如果我们在行末停止，需要写入换行符
		   使光标能正确移动到下一行。
		*/
		t.outBuf = append(t.outBuf, '\r', '\n')
	}
}

// eraseNPreviousChars 删除光标前n个字符
// 参数:
//   - n: 要删除的字符数
func (t *Terminal) eraseNPreviousChars(n int) {
	if n == 0 { // 无需删除
		return
	}

	// 确保不会删除超过行首位置
	if t.pos < n {
		n = t.pos
	}

	// 更新光标位置
	t.pos -= n
	t.moveCursorToPos(t.pos)

	// 移动剩余字符覆盖被删除部分
	copy(t.line[t.pos:], t.line[n+t.pos:])
	t.line = t.line[:len(t.line)-n] // 调整切片长度

	// 回显模式下更新显示
	if t.echo {
		// 重写剩余字符
		t.writeLine(t.line[t.pos:])
		// 用空格覆盖原位置最后n个字符
		for i := 0; i < n; i++ {
			t.queue(space)
		}
		// 移动光标并重新定位
		t.advanceCursor(n)
		t.moveCursorToPos(t.pos)
	}
}

// countToLeftWord 计算从光标位置到前一个单词开头的字符数
// 返回值:
//   - int: 到前一个单词开头的距离
func (t *Terminal) countToLeftWord() int {
	if t.pos == 0 { // 已在行首
		return 0
	}

	pos := t.pos - 1
	// 跳过当前位置前的连续空格
	for pos > 0 {
		if t.line[pos] != ' ' {
			break
		}
		pos--
	}
	// 查找单词起始位置(遇到空格或行首停止)
	for pos > 0 {
		if t.line[pos] == ' ' {
			pos++ // 停在单词第一个字符
			break
		}
		pos--
	}

	return t.pos - pos // 计算距离
}

// countToRightWord 计算从光标位置到下一个单词开头的字符数
// 返回值:
//   - int: 到下一个单词开头的距离
func (t *Terminal) countToRightWord() int {
	pos := t.pos
	// 跳过当前单词剩余部分
	for pos < len(t.line) {
		if t.line[pos] == ' ' {
			break
		}
		pos++
	}
	// 跳过单词间空格
	for pos < len(t.line) {
		if t.line[pos] != ' ' {
			break
		}
		pos++
	}
	return pos - t.pos // 计算距离
}

// visualLength 计算rune切片中可见字符的视觉长度（排除控制字符和转义序列）
// 参数:
//   - runes: 需要计算长度的rune切片
//
// 返回值:
//   - int: 可见字符的实际显示长度
func visualLength(runes []rune) int {
	inEscapeSeq := false // 标记是否处于转义序列中
	length := 0          // 可见字符计数器

	for _, r := range runes {
		switch {
		case inEscapeSeq:
			// 转义序列结束条件：遇到字母字符
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscapeSeq = false
			}
		case r == '\x1b': // ESC键(0x1b)表示转义序列开始
			inEscapeSeq = true
		default:
			// 普通可见字符，计数器增加
			length++
		}
	}

	return length
}

// handleKey 处理用户按键输入，并返回可能的完整输入行
// 参数:
//   - key: 用户按下的rune键值
//
// 返回值:
//   - line: 当按下回车键时返回的完整输入行
//   - ok: 是否返回了有效的输入行
func (t *Terminal) handleKey(key rune) (line string, ok bool) {
	// 粘贴模式特殊处理(除回车键外)
	if t.pasteActive && key != keyEnter {
		t.resetAutoComplete()
		t.addKeyToLine(key)
		return
	}

	// 以下按键会重置自动补全状态
	switch key {
	case keyBackspace, keyAltLeft, keyAltRight, keyLeft, keyRight,
		keyHome, keyEnd, keyDel, keyUp, keyDown, keyEnter,
		keyDeleteWord, keyDeleteLine, keyCtrlD, keyCtrlU, keyClearScreen:
		t.resetAutoComplete()
	}

	// 处理各种控制键
	switch key {
	case keyDel: // Delete键处理
		if t.pos >= len(t.line) || len(t.line) == 0 {
			return
		}
		t.pos += 1
		t.eraseNPreviousChars(1)

	case keyBackspace: // 退格键处理
		if t.pos == 0 {
			return
		}
		t.eraseNPreviousChars(1)

	case keyAltLeft: // Alt+左箭头(前移一个单词)
		t.pos -= t.countToLeftWord()
		t.moveCursorToPos(t.pos)

	case keyAltRight: // Alt+右箭头(后移一个单词)
		t.pos += t.countToRightWord()
		t.moveCursorToPos(t.pos)

	case keyLeft: // 左箭头键
		if t.pos == 0 {
			return
		}
		t.pos--
		t.moveCursorToPos(t.pos)

	case keyRight: // 右箭头键
		if t.pos == len(t.line) {
			return
		}
		t.pos++
		t.moveCursorToPos(t.pos)

	case keyHome: // Home键(移动到行首)
		if t.pos == 0 {
			return
		}
		t.pos = 0
		t.moveCursorToPos(t.pos)

	case keyEnd: // End键(移动到行尾)
		if t.pos == len(t.line) {
			return
		}
		t.pos = len(t.line)
		t.moveCursorToPos(t.pos)

	case keyUp: // 上箭头(历史记录上一条)
		entry, ok := t.history.NthPreviousEntry(t.historyIndex + 1)
		if !ok {
			return "", false
		}
		if t.historyIndex == -1 {
			t.historyPending = string(t.line) // 保存当前未提交行
		}
		t.historyIndex++
		runes := []rune(entry)
		t.setLine(runes, len(runes))

	case keyDown: // 下箭头(历史记录下一条)
		switch t.historyIndex {
		case -1: // 无历史记录
			return
		case 0: // 回到未提交的行
			runes := []rune(t.historyPending)
			t.setLine(runes, len(runes))
			t.historyIndex--
		default: // 其他历史记录
			entry, ok := t.history.NthPreviousEntry(t.historyIndex - 1)
			if ok {
				t.historyIndex--
				runes := []rune(entry)
				t.setLine(runes, len(runes))
			}
		}

	case keyEnter: // 回车键(提交输入)
		t.moveCursorToPos(len(t.line))
		t.queue([]rune("\r\n"))
		line = string(t.line)
		ok = true
		// 重置行状态
		t.line = t.line[:0]
		t.pos = 0
		t.cursorX = 0
		t.cursorY = 0
		t.maxLine = 0

	case keyDeleteWord: // 删除前一个单词
		t.eraseNPreviousChars(t.countToLeftWord())

	case keyDeleteLine: // 删除至行尾
		for i := t.pos; i < len(t.line); i++ {
			t.queue(space)
			t.advanceCursor(1)
		}
		t.line = t.line[:t.pos]
		t.moveCursorToPos(t.pos)

	case keyCtrlD: // Ctrl+D(删除光标下字符或EOF)
		if t.pos < len(t.line) {
			t.pos++
			t.eraseNPreviousChars(1)
		}

	case keyCtrlU: // Ctrl+U(删除至行首)
		t.eraseNPreviousChars(t.pos)

	case keyClearScreen: // 清屏(Ctrl+L)
		t.queue([]rune("\x1b[2J\x1b[H")) // 清屏并移动光标到左上角
		t.queue(t.prompt)
		t.cursorX, t.cursorY = 0, 0
		t.advanceCursor(visualLength(t.prompt))
		t.setLine(t.line, t.pos)

	case keyCtrlC: // Ctrl+C(中断当前输入)
		t.queue([]rune("^C\r\n"))
		t.queue(t.prompt)
		t.cursorX = 0
		t.advanceCursor(visualLength(t.prompt))
		t.setLine([]rune{}, 0)

	default: // 其他字符处理
		if t.AutoCompleteCallback != nil { // 自动补全回调
			prefix := string(t.line[:t.pos])
			suffix := string(t.line[t.pos:])

			t.lock.Unlock()
			newLine, newPos, completeOk := t.AutoCompleteCallback(t, prefix+suffix, len(prefix), key)
			t.lock.Lock()

			if completeOk {
				t.setLine([]rune(newLine), utf8.RuneCount([]byte(newLine)[:newPos]))
				return
			}
		}

		// 非可打印字符或超过最大长度限制
		if !isPrintable(key) || len(t.line) == maxLineLength {
			return
		}

		// 添加普通字符到当前行
		t.addKeyToLine(key)
	}
	return
}

// Clear 清空终端屏幕并重置光标位置
// 线程安全：使用互斥锁保护终端状态
func (t *Terminal) Clear() {
	t.lock.Lock()
	defer t.lock.Unlock()

	// 发送VT100控制序列：
	// \x1b[2J - 清除整个屏幕
	// \x1b[H  - 移动光标到左上角(0,0)位置
	t.queue([]rune("\x1b[2J\x1b[H"))

	// 重新显示提示符
	t.queue(t.prompt)

	// 重置光标位置
	t.cursorX, t.cursorY = 0, 0

	// 调整光标位置(考虑提示符长度)
	t.advanceCursor(visualLength(t.prompt))

	// 重新显示当前输入行
	t.setLine(t.line, t.pos)
}

// addKeyToLine 在当前行的光标位置插入字符
// 参数:
//   - key: 要插入的rune字符
func (t *Terminal) addKeyToLine(key rune) {
	// 如果行缓冲区已满，扩容为原来的2倍
	if len(t.line) == cap(t.line) {
		newLine := make([]rune, len(t.line), 2*(1+len(t.line)))
		copy(newLine, t.line)
		t.line = newLine
	}

	// 扩展切片长度并后移右侧字符
	t.line = t.line[:len(t.line)+1]
	copy(t.line[t.pos+1:], t.line[t.pos:])

	// 插入新字符
	t.line[t.pos] = key

	// 回显模式下更新显示
	if t.echo {
		t.writeLine(t.line[t.pos:])
	}

	// 移动光标到新位置
	t.pos++
	t.moveCursorToPos(t.pos)
}

// writeLine 向终端写入一行内容，处理自动换行
// 参数:
//   - line: 要写入的rune切片
func (t *Terminal) writeLine(line []rune) {
	for len(line) != 0 {
		// 计算当前行剩余空间
		remainingOnLine := t.termWidth - t.cursorX
		todo := len(line)

		// 如果内容超过剩余空间，则截断
		if todo > remainingOnLine {
			todo = remainingOnLine
		}

		// 写入当前行可容纳的内容
		t.queue(line[:todo])

		// 更新光标位置(考虑多字节字符的视觉长度)
		t.advanceCursor(visualLength(line[:todo]))

		// 处理剩余内容
		line = line[todo:]
	}
}

// writeWithCRLF 写入数据并将所有\n替换为\r\n
// 参数:
//   - w: 目标写入器
//   - buf: 要写入的字节切片
//
// 返回值:
//   - n: 实际写入的字节数
//   - err: 写入过程中遇到的错误
func writeWithCRLF(w io.Writer, buf []byte) (n int, err error) {
	for len(buf) > 0 {
		// 查找下一个换行符位置
		i := bytes.IndexByte(buf, '\n')
		todo := len(buf)
		if i >= 0 {
			todo = i
		}

		// 写入换行符之前的内容
		var nn int
		nn, err = w.Write(buf[:todo])
		n += nn
		if err != nil {
			return n, err
		}
		buf = buf[todo:]

		// 如果找到换行符，替换为CRLF
		if i >= 0 {
			if _, err = w.Write(crlf); err != nil {
				return n, err
			}
			n++           // 计数增加1(因为替换\n为\r\n)
			buf = buf[1:] // 跳过已处理的\n
		}
	}

	return n, nil
}

// Write 向终端写入数据，处理光标位置和换行符转换
// 参数:
//   - buf: 要写入的字节数据
//
// 返回值:
//   - n: 实际写入的字节数
//   - err: 写入过程中遇到的错误
func (t *Terminal) Write(buf []byte) (n int, err error) {
	// 原始模式直接写入底层连接
	if t.raw {
		return t.c.Write(buf)
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	// 简单情况：光标在起始位置，直接写入并转换换行符
	if t.cursorX == 0 && t.cursorY == 0 {
		return writeWithCRLF(t.c, buf)
	}

	// 复杂情况：需要处理已有提示符和用户输入
	// 1. 清除当前行光标右侧内容
	t.move(0 /* 不上移 */, 0 /* 不下移 */, t.cursorX /* 左移到行首 */, 0 /* 不右移 */)
	t.cursorX = 0
	t.clearLineToRight()

	// 2. 清除上方所有行
	for t.cursorY > 0 {
		t.move(1 /* 上移一行 */, 0, 0, 0)
		t.cursorY--
		t.clearLineToRight()
	}

	// 3. 先输出缓冲区内容
	if _, err = t.c.Write(t.outBuf); err != nil {
		return
	}
	t.outBuf = t.outBuf[:0]

	// 4. 写入新数据(转换换行符)
	if n, err = writeWithCRLF(t.c, buf); err != nil {
		return
	}

	// 5. 重新显示提示符和当前输入行
	t.writeLine(t.prompt)
	if t.echo {
		t.writeLine(t.line)
	}

	// 6. 恢复光标位置
	t.moveCursorToPos(t.pos)

	// 7. 输出缓冲区内容
	if _, err = t.c.Write(t.outBuf); err != nil {
		return
	}
	t.outBuf = t.outBuf[:0]
	return
}

// ReadPassword 读取密码输入(无回显)
// 参数:
//   - prompt: 临时提示符
//
// 返回值:
//   - line: 读取到的密码字符串
//   - err: 读取过程中遇到的错误
func (t *Terminal) ReadPassword(prompt string) (line string, err error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	// 保存原提示符并设置临时提示符
	oldPrompt := t.prompt
	t.prompt = []rune(prompt)
	t.echo = false // 禁用回显

	line, err = t.readLine()

	// 恢复原提示符和回显设置
	t.prompt = oldPrompt
	t.echo = true

	return
}

// ReadLine 从终端读取一行输入
// 返回值:
//   - line: 读取到的输入行
//   - err: 读取过程中遇到的错误
func (t *Terminal) ReadLine() (line string, err error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.readLine()
}

// readLine 从终端读取一行输入（内部方法，调用前需加锁）
// 返回值:
//   - line: 读取到的输入行
//   - err: 读取过程中遇到的错误
func (t *Terminal) readLine() (line string, err error) {
	// 注意：调用此方法时 t.lock 必须已被锁定

	// 初始化显示提示符（如果光标在起始位置）
	if t.cursorX == 0 && t.cursorY == 0 {
		t.writeLine(t.prompt)
		t.c.Write(t.outBuf)
		t.outBuf = t.outBuf[:0]
	}

	// 标记当前行是否为粘贴内容
	lineIsPasted := t.pasteActive

	// 处理原始模式溢出的数据
	if t.rawOverflow != nil {
		data, ok := <-t.rawOverflow
		if ok {
			n := copy(t.inBuf[:], data)
			t.remainder = t.inBuf[:n]
			close(t.rawOverflow)
		}
		t.rawOverflow = nil
	}

	// 主读取循环
	for {
		rest := t.remainder
		lineOk := false

		// 处理缓冲区中已有的按键序列
		for !lineOk {
			var key rune
			key, rest = bytesToKey(rest, t.pasteActive)

			if key == utf8.RuneError { // 无效UTF-8序列
				break
			}

			// 非粘贴模式下的特殊按键处理
			if !t.pasteActive {
				if key == keyCtrlD { // Ctrl+D处理
					if len(t.line) == 0 {
						return "", ErrCtrlD
					}
				}
				if key == keyPasteStart { // 粘贴开始标记
					t.pasteActive = true
					if len(t.line) == 0 {
						lineIsPasted = true
					}
					continue
				}
			} else if key == keyPasteEnd { // 粘贴结束标记
				t.pasteActive = false
				continue
			}

			// 更新粘贴状态标记
			if !t.pasteActive {
				lineIsPasted = false
			}

			// 处理按键并获取可能的完成行
			line, lineOk = t.handleKey(key)
		}

		// 更新剩余未处理字节
		if len(rest) > 0 {
			n := copy(t.inBuf[:], rest)
			t.remainder = t.inBuf[:n]
		} else {
			t.remainder = nil
		}

		// 输出缓冲区内容
		t.c.Write(t.outBuf)
		t.outBuf = t.outBuf[:0]

		// 如果获得完整行则返回
		if lineOk {
			if t.echo {
				t.historyIndex = -1
				line2 := strings.TrimSpace(line)
				if line2 != "" {
					t.history.Add(line2) // 添加到历史记录
				}
			}
			if lineIsPasted {
				err = ErrPasteIndicator // 标记为粘贴内容
			}
			return
		}

		// 准备读取更多数据
		// t.remainder 是 t.inBuf 开头包含的部分按键序列
		readBuf := t.inBuf[len(t.remainder):]
		var n int

		// 临时解锁以执行阻塞读取
		t.lock.Unlock()
		n, err = t.c.Read(readBuf)
		t.lock.Lock()

		if err != nil {
			return
		}

		// 合并新旧数据
		t.remainder = t.inBuf[:n+len(t.remainder)]
	}
}

// SetPrompt 设置终端提示符，用于后续行读取
// 参数:
//   - prompt: 要设置的提示字符串
func (t *Terminal) SetPrompt(prompt string) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.prompt = []rune(prompt) // 转换为rune切片存储
}

// clearAndRepaintLinePlusNPrevious 清除并重绘当前行及前N行
// 参数:
//   - numPrevLines: 需要重绘的前行数
func (t *Terminal) clearAndRepaintLinePlusNPrevious(numPrevLines int) {
	// 移动光标到行首
	t.move(t.cursorY, 0, t.cursorX, 0)
	t.cursorX, t.cursorY = 0, 0
	t.clearLineToRight() // 清除当前行

	// 清除并下移指定行数
	for t.cursorY < numPrevLines {
		t.move(0, 1, 0, 0)
		t.cursorY++
		t.clearLineToRight()
	}

	// 移动回起始位置
	t.move(t.cursorY, 0, 0, 0)
	t.cursorX, t.cursorY = 0, 0

	// 重绘提示符和当前行
	t.queue(t.prompt)
	t.advanceCursor(visualLength(t.prompt))
	t.writeLine(t.line)
	t.moveCursorToPos(t.pos) // 恢复光标位置
}

// SetSize 设置终端尺寸并处理显示调整
// 参数:
//   - width: 新宽度(字符数)
//   - height: 新高度(行数)
//
// 返回值:
//   - error: 调整过程中发生的错误
func (t *Terminal) SetSize(width, height int) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	// 确保最小宽度为1
	if width == 0 {
		width = 1
	}

	oldWidth := t.termWidth
	t.termWidth, t.termHeight = width, height // 更新尺寸

	switch {
	case width == oldWidth:
		// 宽度未变化，无需处理
		return nil
	case len(t.line) == 0 && t.cursorX == 0 && t.cursorY == 0:
		// 空行且光标在起始位置，无需处理
		return nil
	case width < oldWidth:
		/*
		   终端宽度缩小处理：
		   - xterm类终端会截断过长行
		   - gnome-terminal类终端会尝试折行
		   这里假设是折行终端，调整光标位置计算方式
		*/
		if t.cursorX >= t.termWidth {
			t.cursorX = t.termWidth - 1
		}
		t.cursorY *= 2 // 考虑折行导致的行数倍增
		t.clearAndRepaintLinePlusNPrevious(t.maxLine * 2)
	case width > oldWidth:
		/*
		   终端宽度扩大处理：
		   由于之前可能有折行，现在需要重新计算布局
		   通过完全重绘确保显示正确
		*/
		t.clearAndRepaintLinePlusNPrevious(t.maxLine)
	}

	// 写入输出缓冲区内容并清空
	_, err := t.c.Write(t.outBuf)
	t.outBuf = t.outBuf[:0]
	return err
}

// pasteIndicatorError 表示粘贴指示器错误的类型
type pasteIndicatorError struct{}

// Error 实现error接口，返回错误描述
func (pasteIndicatorError) Error() string {
	return "终端: 未正确处理ErrPasteIndicator"
}

// ErrPasteIndicator 可能由ReadLine返回，表示当前行是粘贴内容
// 程序可能需要以更字面的方式处理粘贴内容(相比手动输入)
var ErrPasteIndicator = pasteIndicatorError{}

// SetBracketedPasteMode 设置终端是否启用括号粘贴模式
// 参数:
//   - on: 是否启用该模式
//
// 说明:
//  1. 不是所有终端都支持此模式
//  2. 启用后会阻止粘贴内容触发自动补全回调
//  3. 完全粘贴的行会返回ErrPasteIndicator错误
func (t *Terminal) SetBracketedPasteMode(on bool) {
	if on {
		io.WriteString(t.c, "\x1b[?2004h") // 启用括号粘贴模式
	} else {
		io.WriteString(t.c, "\x1b[?2004l") // 禁用括号粘贴模式
	}
}

// stRingBuffer 字符串环形缓冲区实现
type stRingBuffer struct {
	entries []string // 存储元素的数组
	max     int      // 最大容量
	head    int      // 最新元素的索引
	size    int      // 当前元素数量
}

// Add 向环形缓冲区添加字符串
func (s *stRingBuffer) Add(a string) {
	// 延迟初始化
	if s.entries == nil {
		const defaultNumEntries = 100
		s.entries = make([]string, defaultNumEntries)
		s.max = defaultNumEntries
	}

	// 计算新位置并存储
	s.head = (s.head + 1) % s.max
	s.entries[s.head] = a

	// 更新当前大小(不超过最大值)
	if s.size < s.max {
		s.size++
	}
}

// NthPreviousEntry 获取第n个最近添加的元素
// 参数:
//   - n: 回溯的步数(0表示最近一个)
//
// 返回值:
//   - value: 找到的元素值
//   - ok: 是否成功找到
func (s *stRingBuffer) NthPreviousEntry(n int) (value string, ok bool) {
	if n >= s.size {
		return "", false
	}
	// 计算环形索引
	index := s.head - n
	if index < 0 {
		index += s.max
	}
	return s.entries[index], true
}

// resetAutoComplete 重置自动补全状态
func (t *Terminal) resetAutoComplete() {
	t.autoCompleteIndex = 0
	t.autoCompletePendng = ""
	t.autoCompleting = false
	t.autoCompletePos = 0
}

// startAutoComplete 初始化自动补全状态
// 参数:
//   - lineFragment: 当前输入的片段
//   - pos: 光标位置
func (t *Terminal) startAutoComplete(lineFragment string, pos int) {
	t.autoCompleteIndex = 0
	t.autoCompletePendng = lineFragment
	t.autoCompleting = true
	t.autoCompletePos = pos
}
