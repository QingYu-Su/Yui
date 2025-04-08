// 构建约束：只有在没有定义nologging标签时才编译此文件
//go:build !nologging
// +build !nologging

package logger

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
)

// Ulogf 是核心日志记录方法，处理实际的日志输出
// 参数：
//
//	callerStackDepth - 调用栈深度（用于定位调用位置）
//	u - 日志紧急程度/级别
//	format - 格式化字符串
//	v - 格式化参数
func (l *Logger) Ulogf(callerStackDepth int, u Urgency, format string, v ...interface{}) {
	// 检查当前日志级别是否需要记录此消息
	// 如果请求级别低于全局级别或全局级别为DISABLE则直接返回
	if u < globalLevel || globalLevel == DISABLE {
		return
	}

	// 获取调用者信息（文件、行号、函数名）
	pc, file, line, ok := runtime.Caller(callerStackDepth)
	if !ok {
		// 如果获取调用信息失败，使用默认值
		file = "?"
		line = 0
	}

	// 从程序计数器获取函数信息
	fn := runtime.FuncForPC(pc)
	var fnName string
	if fn == nil {
		// 无法获取函数信息时使用默认值
		fnName = "?()"
	} else {
		// 提取并格式化函数名（只保留最后一部分）
		dotName := filepath.Ext(fn.Name())
		fnName = strings.TrimLeft(dotName, ".") + "()"
	}

	// 格式化日志消息内容
	msg := fmt.Sprintf(format, v...)
	// 构建完整日志前缀：[ID] 级别 文件名:行号 函数名 :
	prefix := fmt.Sprintf("[%s] %s %s:%d %s : ",
		l.id,                // 日志器ID
		urgency(u),          // 级别字符串
		filepath.Base(file), // 仅保留文件名（不含路径）
		line,                // 行号
		fnName)              // 函数名

	// 输出日志（自动添加换行）
	log.Print(prefix, msg, "\n")

	// 如果是FATAL级别，触发panic终止程序
	if u == FATAL {
		panic("Log was used with FATAL")
	}
}
