package commands

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/QingYu-Su/Yui/internal/server/observers"
	"github.com/QingYu-Su/Yui/internal/server/users"
	"github.com/QingYu-Su/Yui/internal/terminal"
	"github.com/fatih/color"
)

// watch 结构体定义了一个监控命令，用于查看连接事件
// datadir 字段指定了存储连接事件数据的目录路径
type watch struct {
	datadir string
}

// ValidArgs 返回watch命令的有效参数及其描述
// 返回一个map，其中:
//   - "a": 列出所有之前的连接事件
//   - "l": 列出指定数量的最近连接事件，例如 watch -l 10 显示最后10个连接
func (w *watch) ValidArgs() map[string]string {
	return map[string]string{
		"a": "Lists all previous connection events",
		"l": "List previous n number of connection events, e.g watch -l 10 shows last 10 connections",
	}
}

// Run 方法是 watch 命令的主要执行逻辑
// 根据不同的参数选项执行不同的监控功能
func (w *watch) Run(user *users.User, tty io.ReadWriter, line terminal.ParsedLine) error {
	// 处理 -a 参数：显示所有历史连接记录
	if line.IsSet("a") {
		// 打开日志文件
		f, err := os.Open(filepath.Join(w.datadir, "watch.log"))
		if err != nil {
			log.Println("unable to open watch.log:", err)
			return err
		}
		defer f.Close()

		// 逐行读取并输出日志内容
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fmt.Fprintf(tty, "%s\n\r", sc.Text())
		}

		return sc.Err()
	}

	// 处理 -l 参数：显示指定数量的最近连接记录
	if numberOfLinesStr, err := line.GetArgString("l"); err == nil {
		// 打开日志文件
		f, err := os.Open(filepath.Join(w.datadir, "watch.log"))
		if err != nil {
			log.Println("unable to open watch.log:", err)
			return err
		}
		defer f.Close()

		// 获取文件信息
		info, err := f.Stat()
		if err != nil {
			return err
		}

		// 将参数转换为整数
		numberOfLines, err := strconv.Atoi(numberOfLinesStr)
		if err != nil {
			return err
		}

		// 从文件末尾开始反向读取，寻找指定行数的起始位置
		readStartIndex := info.Size()
		i := 0
	outer:
		for {
			// 每次向后移动128字节
			readStartIndex -= 128
			if readStartIndex < 0 {
				readStartIndex = 0
			}

			// 读取128字节的数据块
			buffer := make([]byte, 128)
			n, err := f.ReadAt(buffer, readStartIndex)
			if err != nil {
				if err == io.EOF {
					break outer
				}
				return err
			}

			// 反向扫描查找换行符
			for ii := n - 1; ii > 0; ii-- {
				if buffer[ii] == '\n' {
					i++
				}

				// 当找到足够数量的行时，确定读取起始位置
				if i == numberOfLines+1 {
					readStartIndex += int64(ii) + 1
					break outer
				}
			}

			// 如果已到达文件开头，则退出循环
			if readStartIndex == 0 {
				break
			}
		}

		// 定位到计算出的起始位置
		_, err = f.Seek(readStartIndex, 0)
		if err != nil {
			return err
		}

		// 从起始位置开始读取并输出日志内容
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fmt.Fprintf(tty, "%s\n\r", scanner.Text())
		}

		return scanner.Err()
	}

	// 如果没有指定参数，则实时监控连接状态
	messages := make(chan string)

	// 注册连接状态观察者
	observerId := observers.ConnectionState.Register(func(c observers.ClientState) {
		// 根据连接状态设置箭头方向和颜色
		var arrowDirection = "<-"
		if c.Status == "disconnected" {
			arrowDirection = "->"
			// 断开连接显示为红色
			messages <- fmt.Sprintf("%s %s %s (%s %s) %s %s",
				c.Timestamp.Format("2006/01/02 15:04:05"),
				arrowDirection,
				color.BlueString(c.HostName),
				c.IP,
				color.YellowString(c.ID),
				c.Version,
				color.RedString(c.Status))
		} else {
			// 连接状态显示为绿色
			messages <- fmt.Sprintf("%s %s %s (%s %s) %s %s",
				c.Timestamp.Format("2006/01/02 15:04:05"),
				arrowDirection,
				color.BlueString(c.HostName),
				c.IP,
				color.YellowString(c.ID),
				c.Version,
				color.GreenString(c.Status))
		}
	})

	// 如果终端支持原始模式，则启用
	term, isTerm := tty.(*terminal.Terminal)
	if isTerm {
		term.EnableRaw()
	}

	// 启动goroutine监听用户输入以退出监控
	go func() {
		b := make([]byte, 1)
		tty.Read(b)                                      // 等待任意按键
		observers.ConnectionState.Deregister(observerId) // 注销观察者
		close(messages)                                  // 关闭消息通道
	}()

	// 开始监控
	fmt.Fprintf(tty, "Watching clients...\n\r")
	for m := range messages {
		fmt.Fprintf(tty, "%s\n\r", m) // 输出连接状态变化
	}

	// 恢复终端原始模式
	if isTerm {
		term.DisableRaw()
	}

	return nil
}

// Expect 方法定义了命令期望的参数列表
// 对于watch命令来说，不需要任何自动补全建议，所以返回nil
func (w *watch) Expect(line terminal.ParsedLine) []string {
	return nil
}

// Help 方法返回命令的帮助信息
// explain参数控制返回信息的详细程度：
//   - 当explain为true时，返回简短的命令描述
//   - 当explain为false时，返回格式化的完整帮助文本
func (w *watch) Help(explain bool) string {
	if explain {
		return "Watches controllable client connections"
	}

	// 使用terminal包中的MakeHelpText函数生成格式化的帮助文本
	return terminal.MakeHelpText(
		w.ValidArgs(),     // 获取命令有效参数
		"watch [OPTIONS]", // 命令使用格式
		"Watch shows continuous connection status of clients (prints the joining and leaving of clients)", // 主要描述
		"Defaultly waits for new connection events",                                                       // 补充说明
	)
}

// Watch 是watch命令的构造函数
// 接收一个datadir参数指定数据目录路径
// 返回初始化好的watch命令实例
func Watch(datadir string) *watch {
	return &watch{datadir: datadir}
}
