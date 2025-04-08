// 构建约束：只有在定义了nologging标签时才编译此文件
// 这个文件是logger包的"空实现"版本，用于完全禁用日志功能
//go:build nologging
// +build nologging

package logger

// Ulogf 的空实现方法，当启用nologging构建标签时使用
// 此版本的方法不做任何实际操作，相当于完全禁用日志功能
// 参数：
//
//	callerStackDepth - 调用栈深度（此处不使用）
//	u - 日志级别（此处不使用）
//	format - 格式化字符串（此处不使用）
//	v - 格式化参数（此处不使用）
func (l *Logger) Ulogf(callerStackDepth int, u Urgency, format string, v ...interface{}) {
	// 空实现 - 不执行任何操作
	// 当使用nologging标签构建时，所有日志调用都会指向这个空方法
	// 这样可以完全消除日志记录带来的性能开销
}
