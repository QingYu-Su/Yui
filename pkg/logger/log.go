package logger

import (
	"fmt"
	"strings"
)

// Urgency 定义日志级别类型
type Urgency int

// 日志级别常量定义
const (
	DISABLE         = 0    // 禁用所有日志
	INFO    Urgency = iota // 信息级别(最低级别)
	WARN                   // 警告级别
	ERROR                  // 错误级别
	FATAL                  // 致命错误级别(最高级别)
)

// 全局日志级别，默认为INFO
var globalLevel Urgency = INFO

// Logger 日志记录器结构体
type Logger struct {
	id string // 日志标识符，用于区分不同模块的日志
}

// SetLogLevel 设置全局日志级别
func SetLogLevel(level Urgency) {
	globalLevel = level
}

// GetLogLevel 获取当前全局日志级别
func GetLogLevel() Urgency {
	return globalLevel
}

// Info 记录信息级别日志
func (l *Logger) Info(format string, v ...interface{}) {
	l.Ulogf(2, INFO, format, v...)
}

// Warning 记录警告级别日志
func (l *Logger) Warning(format string, v ...interface{}) {
	l.Ulogf(2, WARN, format, v...)
}

// Error 记录错误级别日志
func (l *Logger) Error(format string, v ...interface{}) {
	l.Ulogf(2, ERROR, format, v...)
}

// Fatal 记录致命错误级别日志
func (l *Logger) Fatal(format string, v ...interface{}) {
	l.Ulogf(2, FATAL, format, v...)
}

// urgency 将日志级别枚举转换为可读字符串
func urgency(u Urgency) string {
	switch u {
	case INFO:
		return "INFO"
	case WARN:
		return "WARNING"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	case DISABLE:
		return "DISABLED"
	}

	return "UNKNOWN_URGENCY"
}

// StrToUrgency 将字符串转换为日志级别枚举
func StrToUrgency(s string) (Urgency, error) {
	s = strings.ToUpper(s) // 转换为大写以支持大小写不敏感

	switch s {
	case "INFO":
		return INFO, nil
	case "WARNING", "WARN": // 支持两种警告级别写法
		return WARN, nil
	case "ERROR", "ERR": // 支持两种错误级别写法
		return ERROR, nil
	case "FATAL":
		return FATAL, nil
	case "DISABLED":
		return DISABLE, nil
	}

	// 无效日志级别返回错误
	return 0, fmt.Errorf("urgency %q isn't a valid urgency [INFO,WARNING,ERROR,FATAL,DISABLED]", s)
}

// UrgencyToStr 将日志级别枚举转换为字符串
func UrgencyToStr(u Urgency) string {
	return urgency(u)
}

// NewLog 创建新的日志记录器实例
func NewLog(id string) Logger {
	var l Logger
	l.id = id // 设置日志标识符
	return l
}
