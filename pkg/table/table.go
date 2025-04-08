package table

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// value 表示表格中的一个单元格值
type value struct {
	parts   []string // 单元格内容按行分割后的字符串数组
	longest int      // 单元格中最长一行的长度
}

// Table 表示一个文本表格
type Table struct {
	name          string    // 表格名称
	cols          int       // 列数
	line          [][]value // 表格所有行数据
	cellMaxWidth  []int     // 每列的最大宽度
	lineMaxHeight []int     // 每行的最大高度(行数)
}

// makeValue 将输入字符串转换为value结构体
func makeValue(rn string) (val value) {
	rn = strings.TrimSpace(rn)
	val.parts = strings.Split(rn, "\n") // 按换行符分割内容
	for _, n := range val.parts {
		if len(n) > val.longest {
			val.longest = len(n) // 记录最长行的长度
		}
	}
	return
}

// updateMax 更新表格的最大列宽和行高
// 参数 line 是当前要添加的新行数据(由多个value组成的切片)
// 返回值 error 如果列数不匹配会返回错误
func (t *Table) updateMax(line []value) error {
	// 1. 参数校验：检查新行的列数是否与表格定义的列数一致
	if len(line) != t.cols {
		return errors.New("Number of values exceeds max number of columns")
	}

	// 2. 初始化列宽记录数组(如果是第一次调用)
	if t.cellMaxWidth == nil {
		t.cellMaxWidth = make([]int, t.cols)
	}

	// 3. 计算当前行的高度(遍历所有单元格找出最多行数)
	height := 0
	for i, n := range line {
		// 3.1 更新每列的最大宽度
		// 比较当前单元格最长行与已记录的最大列宽
		if t.cellMaxWidth[i] < n.longest {
			t.cellMaxWidth[i] = n.longest
		}

		// 3.2 计算当前行的高度(取所有单元格行数的最大值)
		if height < len(n.parts) {
			height = len(n.parts)
		}
	}

	// 4. 记录当前行的高度到行高数组
	t.lineMaxHeight = append(t.lineMaxHeight, height)

	return nil
}

// AddValues 向表格添加一行数据
func (t *Table) AddValues(vals ...string) error {
	if len(vals) != t.cols {
		return fmt.Errorf("Error more values exist than number of columns")
	}

	// 将字符串值转换为value结构体
	var line []value
	for _, v := range vals {
		line = append(line, makeValue(v))
	}

	// 更新表格尺寸信息
	err := t.updateMax(line)
	if err != nil {
		return err
	}

	// 添加新行
	t.line = append(t.line, line)

	return nil
}

// seperator 生成表格行分隔线
func (t *Table) seperator() (out string) {
	out = "+"
	for i := 0; i < t.cols; i++ {
		// 每列宽度为最大列宽+2(左右各一个空格)
		out += strings.Repeat("-", t.cellMaxWidth[i]+2) + "+"
	}
	return
}

// Print 将表格输出到标准输出
func (t *Table) Print() {
	t.Fprint(os.Stdout)
}

// Fprint 将表格输出到指定的io.Writer
func (t *Table) Fprint(w io.Writer) {
	for _, line := range t.OutputStrings() {
		fmt.Fprint(w, line+"\n")
	}
}

// FprintWidth 将表格按指定宽度输出到io.Writer
func (t *Table) FprintWidth(w io.Writer, width int) {
	lines := t.OutputStrings()
	for _, line := range lines {
		// 限制每行输出宽度
		for i := 0; i < len(line) && i < width-1; i++ {
			fmt.Fprintf(w, "%c", line[i])
		}
		fmt.Fprint(w, "\n")
	}
}

// OutputStrings 将表格数据转换为可打印的字符串切片
// 返回值 output 是包含表格所有行的字符串数组
func (t *Table) OutputStrings() (output []string) {
	// 1. 生成分隔线(如 "+-------+-------+")
	seperator := t.seperator()

	// 2. 遍历表格中的每一行数据
	for n, line := range t.line {
		// 2.1 准备单元格内容：将每列的值转换为字符串切片
		values := make([][]string, len(line))
		for x, m := range line {
			values[x] = m.parts // 获取单元格的多行内容
		}

		// 2.2 处理每行的多行内容(垂直方向)
		for y := 0; y < t.lineMaxHeight[n]; y++ {
			// 开始构建一行字符串
			rowStr := "|"

			// 处理每列内容
			for x := 0; x < len(line); x++ {
				val := ""
				// 如果当前行有内容则获取，否则留空
				if len(values[x]) > y {
					val = values[x][y]
				}
				// 格式化单元格：左对齐，固定宽度
				// 例如：" %-10s " 表示左对齐，宽度10
				rowStr += fmt.Sprintf(" %-"+fmt.Sprintf("%d", t.cellMaxWidth[x])+"s |", val)
			}

			// 将构建好的行加入输出
			output = append(output, rowStr)
		}

		// 2.3 每行数据后添加分隔线
		output = append(output, seperator)
	}

	// 3. 添加表头和顶部边框
	if len(output) > 0 {
		// 3.1 表名居中显示(基于第一行的长度计算居中位置)
		centeredName := fmt.Sprintf("%"+fmt.Sprintf("%d", len(output[0])/2)+"s", t.name)
		// 3.2 将表头和分隔线插入到输出开头
		output = append([]string{centeredName, seperator}, output...)
	}

	return output
}

// NewTable 创建新表格
func NewTable(name string, columnNames ...string) (t Table, err error) {
	var line []value
	for _, name := range columnNames {
		line = append(line, makeValue(name))
	}

	t.cols = len(line) // 设置列数
	t.name = name      // 设置表名

	// 添加表头行
	return t, t.AddValues(columnNames...)
}
