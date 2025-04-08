package trie

import (
	"sync"
)

/*
* 线程安全的前缀树(Trie)实现
* 注意：只有在访问根节点时才是线程安全的(由于Go缺乏可重入锁机制)
 */
type Trie struct {
	root     bool           // 标记是否为根节点
	c        byte           // 当前节点存储的ASCII字符
	children map[byte]*Trie // 子节点映射表(key为ASCII字符)
	mut      sync.RWMutex   // 读写锁(保证线程安全)
}

// AddMultiple 批量添加字符串到Trie
func (t *Trie) AddMultiple(s ...string) {
	for _, item := range s {
		t.Add(item)
	}
}

// RemoveMultiple 批量从Trie中移除字符串
func (t *Trie) RemoveMultiple(s ...string) {
	for _, item := range s {
		t.Remove(item)
	}
}

// Add 向Trie中添加一个字符串
func (t *Trie) Add(s string) {
	// 根节点需要加写锁
	if t.root {
		t.mut.Lock()
		defer t.mut.Unlock()
	}

	// 空字符串处理
	if len(s) == 0 {
		return
	}

	// 如果存在对应子节点，递归添加剩余部分
	if child, ok := t.children[s[0]]; ok {
		child.Add(s[1:])
		return
	}

	// 创建新子节点并递归添加
	newChild := &Trie{
		children: make(map[byte]*Trie),
		c:        s[0], // 存储当前字符
	}
	t.children[s[0]] = newChild
	newChild.Add(s[1:])
}

// getAll 获取当前节点下的所有完整字符串(内部递归方法)
func (t *Trie) getAll() (result []string) {
	// 根节点需要加读锁
	if t.root {
		t.mut.RLock()
		defer t.mut.RUnlock()
	}

	// 叶子节点(没有子节点)，返回当前字符
	if len(t.children) == 0 {
		return []string{string(t.c)}
	}

	// 非叶子节点处理
	prefix := string(t.c) // 当前字符作为前缀
	if t.root {
		prefix = "" // 根节点没有字符前缀
	}

	// 递归收集所有子节点的字符串
	for _, child := range t.children {
		for _, str := range child.getAll() {
			result = append(result, prefix+str)
		}
	}

	return result
}

// PrefixMatch 前缀匹配查询
func (t *Trie) PrefixMatch(prefix string) (result []string) {
	// 根节点需要加读锁
	if t.root {
		t.mut.RLock()
		defer t.mut.RUnlock()
	}

	// 空前缀，返回当前节点下的所有字符串
	if len(prefix) == 0 {
		if len(t.children) == 0 {
			return []string{""} // 空字符串匹配
		}

		// 收集所有子节点的字符串
		for _, child := range t.children {
			result = append(result, child.getAll()...)
		}
		return result
	}

	// 递归匹配前缀
	if child, ok := t.children[prefix[0]]; ok {
		matches := child.PrefixMatch(prefix[1:])
		// 将当前字符添加到匹配结果前
		for i := range matches {
			matches[i] = string(prefix[0]) + matches[i]
		}
		return matches
	}

	return []string{} // 没有匹配项
}

// Remove 从Trie中移除字符串
func (t *Trie) Remove(s string) bool {
	// 根节点需要加写锁
	if t.root {
		t.mut.Lock()
		defer t.mut.Unlock()
	}

	// 空字符串处理
	if len(s) == 0 {
		return len(t.children) == 0 // 如果是叶子节点则可删除
	}

	// 已经是叶子节点
	if len(t.children) == 0 {
		return true
	}

	// 递归删除子节点
	if child, ok := t.children[s[0]]; ok && child.Remove(s[1:]) {
		delete(t.children, s[0])    // 删除子节点映射
		return len(t.children) == 0 // 如果没有其他子节点则可删除当前节点
	}

	return false
}

// NewTrie 创建并初始化一个新的Trie
func NewTrie(values ...string) *Trie {
	t := &Trie{
		children: make(map[byte]*Trie),
		root:     true, // 标记为根节点
	}

	// 批量添加初始值
	for _, v := range values {
		t.Add(v)
	}

	return t
}
