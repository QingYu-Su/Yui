package trie

import (
	"strings"
	"testing"
)

// TestSimpleAdd 测试Trie的基本添加和前缀匹配功能
func TestSimpleAdd(t *testing.T) {
	// 初始化一个新的Trie
	nt := NewTrie()

	// 添加测试数据
	nt.Add("hello world is jordan") // 长句子
	nt.Add("hello frank")           // 相同前缀
	nt.Add("Yeet Yeet Yeet")        // 重复单词
	nt.Add("Yeet Yoot")             // 相似前缀
	nt.Add("Yapple")                // 混合词
	nt.Add("apple")                 // 单独词

	// 测试前缀匹配"hel"应该返回2个结果
	s := nt.PrefixMatch("hel")
	if len(s) != 2 {
		t.Log("Number of matches for 'hel' != 2")
		t.FailNow() // 立即终止测试
	}

	// 验证结果中是否包含预期的完整字符串
	found := false
	for _, m := range s {
		found = found || strings.Contains(m, "lo world is jordan")
	}

	if !found {
		t.Log("Did not find the completion required")
		t.FailNow()
	}
}

// TestSimpleRemove 测试Trie的删除功能
func TestSimpleRemove(t *testing.T) {
	// 初始化一个新的Trie
	nt := NewTrie()

	// 添加相同的测试数据
	nt.Add("hello world is jordan")
	nt.Add("hello frank")
	nt.Add("Yeet Yeet Yeet")
	nt.Add("Yeet Yoot")
	nt.Add("Yapple")
	nt.Add("apple")

	// 测试1: 尝试删除不存在的项("ap")不应该影响数据
	nt.Remove("ap")
	if len(nt.getAll()) != 6 {
		t.Log("Removing of non-existant item caused length change")
		t.FailNow()
	}

	// 获取删除前的所有项
	before := nt.getAll()

	// 测试2: 删除实际存在的项("apple")
	nt.Remove("apple")

	// 获取删除后的所有项
	after := nt.getAll()

	// 验证删除操作的正确性
	for _, n := range before {
		found := false
		// 检查删除后的集合
		for _, nn := range after {
			if nn == n {
				found = true
				break
			}
		}

		// 如果项丢失但不是我们删除的项，则测试失败
		if !found && n != "apple" {
			t.Logf("Removed wrong item...? '%s'\n", n)
			t.FailNow()
		}
	}
}
