package terminal

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrFlagNotSet 表示标志未设置的错误
var ErrFlagNotSet = errors.New("Flag not set")

// Node 接口定义了终端节点的基础行为
type Node interface {
	Value() string // 获取节点的值
	Start() int    // 获取节点在原始字符串中的起始位置
	End() int      // 获取节点在原始字符串中的结束位置
	Type() string  // 获取节点的类型
}

// baseNode 是所有节点类型的基类，包含公共字段和方法
type baseNode struct {
	start, end int    // 节点在原始字符串中的起始和结束位置
	value      string // 节点的值
}

// Value 返回节点的值
func (bn *baseNode) Value() string {
	return bn.value
}

// Start 返回节点在原始字符串中的起始位置
func (bn *baseNode) Start() int {
	return bn.start
}

// End 返回节点在原始字符串中的结束位置
func (bn *baseNode) End() int {
	return bn.end
}

// Argument 表示命令行参数
type Argument struct {
	baseNode // 嵌入baseNode以继承其字段和方法
}

// Type 返回参数的类型名称
func (a Argument) Type() string {
	return "argument"
}

// Cmd 表示命令
type Cmd struct {
	baseNode // 嵌入baseNode以继承其字段和方法
}

// Type 返回命令的类型名称
func (c Cmd) Type() string {
	return "command"
}

// Flag 表示命令行标志
type Flag struct {
	baseNode // 嵌入baseNode以继承其字段和方法

	Args []Argument // 标志的参数列表
	long bool       // 表示是否是长标志(如--flag)
}

// Type 返回标志的类型名称
func (f Flag) Type() string {
	return "flag"
}

// ArgValues 返回标志的所有参数值的字符串切片
func (f *Flag) ArgValues() (out []string) {
	for _, v := range f.Args {
		out = append(out, v.Value())
	}
	return
}

// ParsedLine 表示解析后的命令行
type ParsedLine struct {
	Chunks []string // 原始命令行按空格分割后的片段

	FlagsOrdered []Flag          // 按出现顺序存储的标志
	Flags        map[string]Flag // 标志名到标志的映射

	Arguments []Argument // 命令行参数列表(无论它是否属于某个标志)
	Focus     Node       // 当前光标聚焦的节点(如光标所在位置)

	Section *Flag // 当前光标所在的标志部分(如果是参数，则默认最靠近的左边标志)

	Command *Cmd // 命令部分

	RawLine string // 原始命令行字符串
}

// Empty 检查解析后的命令行是否为空（原始字符串为空）
func (pl *ParsedLine) Empty() bool {
	return pl.RawLine == ""
}

// ArgumentsAsStrings 将所有的参数转换为字符串切片返回
func (pl *ParsedLine) ArgumentsAsStrings() (out []string) {
	for _, v := range pl.Arguments {
		out = append(out, v.Value())
	}
	return
}

// IsSet 检查指定的标志是否在命令行中设置
func (pl *ParsedLine) IsSet(flag string) bool {
	_, ok := pl.Flags[flag]
	return ok
}

// ExpectArgs 检查标志是否存在并验证其参数数量是否符合预期
// 如果符合则返回参数列表，否则返回错误
func (pl *ParsedLine) ExpectArgs(flag string, needs int) ([]Argument, error) {
	f, ok := pl.Flags[flag]
	if ok {
		if len(f.Args) != needs {
			return nil, fmt.Errorf("flag: %s expects %d arguments", flag, needs)
		}
		return f.Args, nil
	}
	return nil, ErrFlagNotSet
}

// GetArgs 获取指定标志的所有参数
// 如果标志不存在则返回错误
func (pl *ParsedLine) GetArgs(flag string) ([]Argument, error) {
	f, ok := pl.Flags[flag]
	if ok {
		return f.Args, nil
	}
	return nil, ErrFlagNotSet
}

// GetArgsString 获取指定标志的所有参数值（字符串形式）
// 如果标志不存在则返回错误
func (pl *ParsedLine) GetArgsString(flag string) ([]string, error) {
	f, ok := pl.Flags[flag]
	if ok {
		return f.ArgValues(), nil
	}
	return nil, ErrFlagNotSet
}

// GetArg 获取指定标志的单个参数
// 如果标志不存在或参数数量不为1则返回错误
func (pl *ParsedLine) GetArg(flag string) (Argument, error) {
	arg, err := pl.ExpectArgs(flag, 1)
	if err != nil {
		return Argument{}, err
	}
	return arg[0], nil
}

// GetArgString 获取指定标志的单个参数值（字符串形式）
// 如果标志不存在或没有参数则返回错误
func (pl *ParsedLine) GetArgString(flag string) (string, error) {
	f, ok := pl.Flags[flag]
	if !ok {
		return "", ErrFlagNotSet
	}

	if len(f.Args) == 0 {
		return "", fmt.Errorf("flag: %s expects at least 1 argument", flag)
	}
	return f.Args[0].Value(), nil
}

// parseFlag 解析命令行中的标志(flag)，支持-短标志和--长标志
// 参数:
//
//	line - 原始命令行字符串
//	startPos - 开始解析的位置
//
// 返回值:
//
//	f - 解析出的Flag对象
//	endPos - 解析结束的位置(下一个字符的索引)
func parseFlag(line string, startPos int) (f Flag, endPos int) {
	f.start = startPos
	linked := true // 用于跟踪是否还在处理连续的'-'字符

	// 遍历从startPos开始的字符
	for f.end = startPos; f.end < len(line); f.end++ {
		endPos = f.end

		// 遇到空格表示flag结束
		if line[f.end] == ' ' {
			return
		}

		// 处理连续的'-'字符(如--flag中的--)
		if line[f.end] == '-' && linked {
			continue
		}

		// 如果flag长度大于1且之前有'-'，则标记为长flag
		if f.end-startPos > 1 && linked {
			f.long = true
		}

		linked = false // 遇到非'-'字符后不再检查连续的'-'

		// 将字符添加到flag值中
		f.value += string(line[f.end])
	}

	return
}

// parseSingleArg 解析命令行中的单个参数，支持转义和双引号单引号处理
// 参数:
//
//	line - 原始命令行字符串
//	startPos - 开始解析的位置
//
// 返回值:
//
//	arg - 解析出的Argument对象
//	endPos - 解析结束的位置(下一个字符的索引)
func parseSingleArg(line string, startPos int) (arg Argument, endPos int) {
	var (
		inSingleQuote = false // 是否在单引号中
		inDoubleQuote = false // 是否在双引号中
		escaped       = false // 是否处理转义字符
	)

	arg.start = startPos
	var sb strings.Builder // 用于高效构建参数字符串

	// 确保在函数返回前设置arg.value
	defer func() {
		arg.value = sb.String()
	}()

	// 遍历从startPos开始的字符
	for arg.end = startPos; arg.end < len(line); arg.end++ {
		endPos = arg.end
		c := line[endPos]

		// 处理参数结束条件(不在引号中且未转义的空格)
		if !inSingleQuote && !inDoubleQuote && !escaped && c == ' ' {
			return
		}

		// 处理转义字符(不在引号中且遇到反斜杠)
		if !inSingleQuote && !escaped && c == '\\' {
			escaped = true
			continue
		}

		// 处理引号(不在转义状态下)
		if !escaped {
			// 处理单引号(不在双引号中)
			if c == '\'' && !inDoubleQuote {
				inSingleQuote = !inSingleQuote
				continue
			}
			// 处理双引号(不在单引号中)
			if c == '"' && !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
				continue
			}
		}

		// 将字符添加到参数值中
		if escaped {
			// 只对特殊字符进行转义处理，其他则保留斜杠
			if c != '\\' && c != '"' && c != '\'' && c != ' ' {
				sb.WriteByte('\\')
			}
			escaped = false
		}
		sb.WriteByte(c)
		arg.end = endPos
	}

	// 处理输入结束时的未闭合转义
	if escaped {
		sb.WriteByte('\\')
	}

	return
}

// parseArgs 解析命令行中的多个参数
// 参数:
//
//	line - 原始命令行字符串
//	startPos - 开始解析的位置
//
// 返回值:
//
//	args - 解析出的参数列表
//	endPos - 解析结束的位置(下一个字符的索引)
func parseArgs(line string, startPos int) (args []Argument, endPos int) {
	// 从起始位置开始遍历命令行
	for endPos = startPos; endPos < len(line); endPos++ {
		// 解析单个参数
		var arg Argument
		arg, endPos = parseSingleArg(line, endPos)

		// 忽略空值参数
		if len(arg.value) != 0 {
			args = append(args, arg)
		}

		// 如果下一个字符是'-'，表示可能遇到新flag，停止参数收集
		if endPos != len(line)-1 && line[endPos+1] == '-' {
			return
		}
	}

	return
}

// ParseLineValidFlags 解析命令行并验证标志是否在允许的范围内
// 参数:
//
//	line - 原始命令行字符串
//	cursorPosition - 光标当前位置(用于确定焦点元素)
//	validFlags - 允许的标志集合(map[string]bool形式)
//
// 返回值:
//
//	pl - 解析后的命令行结构
//	err - 错误信息(如果发现非法标志)
func ParseLineValidFlags(line string, cursorPosition int, validFlags map[string]bool) (pl ParsedLine, err error) {
	// 先解析原始命令行
	pl = ParseLine(line, cursorPosition)

	// 检查所有标志是否在允许的范围内
	for flag := range pl.Flags {
		_, ok := validFlags[flag]
		if !ok {
			return ParsedLine{}, fmt.Errorf("flag provided but not defined: '%s'", flag)
		}
	}

	return pl, nil
}

// ParseLine 解析完整的命令行字符串并返回结构化结果
// 参数:
//
//	line - 原始命令行字符串
//	cursorPosition - 光标当前位置(用于确定焦点元素)
//
// 返回值:
//
//	pl - 解析后的命令行结构(ParsedLine)
func ParseLine(line string, cursorPosition int) (pl ParsedLine) {
	// 初始化解析状态
	var capture *Flag = nil          // 当前正在捕获参数的flag
	pl.Flags = make(map[string]Flag) // 初始化flag映射表
	pl.RawLine = line                // 保存原始命令行

	// 遍历命令行每个字符
	for i := 0; i < len(line); i++ {
		// 检测到flag起始符'-'
		if line[i] == '-' {
			// 如果之前有正在捕获的flag，先保存它
			if capture != nil {
				// 合并相同flag的参数
				if prev, ok := pl.Flags[capture.Value()]; ok {
					capture.Args = append(capture.Args, prev.Args...)
				}
				// 更新flag映射表和有序列表
				pl.Flags[capture.Value()] = *capture
				pl.FlagsOrdered = append(pl.FlagsOrdered, *capture)
			}

			// 解析新flag
			var newFlag Flag
			newFlag, i = parseFlag(line, i)

			// 检查光标是否在当前flag范围内
			if cursorPosition >= newFlag.start && cursorPosition <= newFlag.end {
				pl.Focus = &newFlag
				pl.Section = &newFlag
			}

			// 记录命令行片段
			pl.Chunks = append(pl.Chunks, pl.RawLine[newFlag.start:newFlag.end])

			// 处理长flag(--开头)
			if newFlag.long {
				capture = &newFlag
				continue
			}

			// 处理短flag(-开头)

			// 单个选项(-l)情况
			if len(newFlag.Value()) == 1 {
				capture = &newFlag
				continue
			}

			// 多个选项组合情况(-ltab)
			capture = nil
			for _, c := range newFlag.Value() {
				var f Flag
				f.start = newFlag.start
				f.end = i
				f.value = string(c)

				// 将每个字符作为独立flag记录
				pl.Flags[f.Value()] = f
				pl.FlagsOrdered = append(pl.FlagsOrdered, f)
			}
			continue
		}

		// 解析参数
		var args []Argument
		args, i = parseArgs(line, i)

		// 处理解析出的参数
		for m, arg := range args {
			pl.Chunks = append(pl.Chunks, arg.value)

			// 检查光标是否在当前参数范围内
			if cursorPosition >= arg.start && cursorPosition <= arg.end {
				pl.Focus = &args[m]
				pl.Section = capture
			}
		}

		// 第一个非flag参数作为命令
		if pl.Command == nil && len(args) > 0 && capture == nil {
			pl.Command = new(Cmd)
			pl.Command.value = args[0].value
			pl.Command.start = args[0].start
			pl.Command.end = args[0].end

			// 检查光标是否在命令范围内
			if cursorPosition >= pl.Command.start && cursorPosition <= pl.Command.end {
				pl.Focus = pl.Command
			}

			args = args[1:] // 剩余参数作为普通参数
		}

		pl.Arguments = append(pl.Arguments, args...)

		// 如果当前在捕获flag状态，将参数关联到该flag
		if capture != nil {
			capture.Args = args
			continue
		}
	}

	// 处理最后一个可能未保存的flag
	if capture != nil {
		if prev, ok := pl.Flags[capture.Value()]; ok {
			capture.Args = append(capture.Args, prev.Args...)
		}
		pl.Flags[capture.Value()] = *capture
		pl.FlagsOrdered = append(pl.FlagsOrdered, *capture)
	}

	// 确定当前section(光标所在区域)
	var closestLeft *Flag
	// 从后向前查找最近的flag
	for i := len(pl.FlagsOrdered) - 1; i >= 0; i-- {
		if cursorPosition >= pl.FlagsOrdered[i].start && cursorPosition <= pl.FlagsOrdered[i].end {
			pl.Section = &pl.FlagsOrdered[i]
			break
		}

		if pl.FlagsOrdered[i].end > cursorPosition {
			continue
		}

		closestLeft = &pl.FlagsOrdered[i]
		break
	}

	// 如果没有精确匹配，使用最近的左侧flag
	if pl.Section == nil && closestLeft != nil {
		pl.Section = closestLeft
	}

	return
}

// MakeHelpText 生成格式化的帮助文本
// 参数:
//
//	flags - 标志及其描述的映射表(map[flag]description)
//	lines - 额外的帮助文本行(可变参数)
//
// 返回值:
//
//	s - 格式化后的完整帮助文本
func MakeHelpText(flags map[string]string, lines ...string) (s string) {
	// 添加额外的帮助文本行
	for _, v := range lines {
		s += v + "\n"
	}

	// 准备标志说明行
	flagLines := []string{}

	// 遍历所有标志生成格式化行
	for flag, description := range flags {
		// 根据flag长度决定前缀(- 或 --)
		prefix := "--"
		if len(flag) == 1 {
			prefix = "-"
		}

		// 格式化为: [tab]前缀flag[tab]描述
		flagLines = append(flagLines, "\t"+prefix+flag+"\t"+description)
	}

	// 对标志行按字母顺序排序
	sort.Strings(flagLines)

	// 组合所有部分并返回
	return s + strings.Join(flagLines, "\n") + "\n"
}
