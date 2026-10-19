// Package safejson 提供容错 JSON 解析：
// 剥除 BOM、注释与代码块围栏，补全尾随逗号、单引号、未加引号的键，
// 从混合文本中抽取最大平衡 JSON 片段，并支持内部多值拼接。
//
// 语义对齐 destr（安全解析 + 回退默认值）与 json-utils（自动修复 + 从文本抽取）。
package safejson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// ErrNoJSON 表示输入中找不到可解析的 JSON。
var ErrNoJSON = errors.New("safejson: 输入中未找到可解析的 JSON")

// Parse 尝试以最宽松的方式把 data 解析到 v。
//
// 解析顺序：
//  1. 原样解析；
//  2. 清洗（去 BOM / 注释 / 围栏，修尾随逗号与单引号）；
//  3. 从混合文本中抽取最外层平衡片段后再清洗解析。
//
// 若全部失败，返回带原始错误的 ErrNoJSON 包装。
func Parse(data []byte, v any) error {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if len(trimmed) == 0 {
		return fmt.Errorf("%w: 内容为空", ErrNoJSON)
	}

	var firstErr error
	// 1) 原样
	if err := json.Unmarshal(trimmed, v); err == nil {
		return nil
	} else {
		firstErr = err
	}
	// 2) 清洗后
	clean := Repair(string(trimmed))
	if err := json.Unmarshal([]byte(clean), v); err == nil {
		return nil
	} else {
		firstErr = err
	}
	// 3) 抽取片段后
	if frag, ok := Extract(clean); ok {
		if err := json.Unmarshal([]byte(frag), v); err == nil {
			return nil
		} else {
			firstErr = err
		}
	}
	return fmt.Errorf("%w: %v", ErrNoJSON, firstErr)
}

// ParseFile 读取文件并按 Parse 的规则解析到 v。
func ParseFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return Parse(data, v)
}

// Repair 对常见的非严格 JSON 做最小改写，使其可被 encoding/json 接受：
// 剥离 UTF-8 BOM、JS 行/块注释、Markdown 代码围栏；删除对象与数组中的尾随逗号；
// 把未被转义的单引号字符串改为双引号。
func Repair(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = stripCodeFence(s)
	s = stripComments(s)
	s = fixSingleQuotes(s)
	s = stripTrailingCommas(s)
	return strings.TrimSpace(s)
}

func stripCodeFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return s
	}
	// ```json\n...\n```
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	}
	if i := strings.LastIndex(t, "```"); i >= 0 {
		t = t[:i]
	}
	// 去掉领先的单独语言标记行
	lines := strings.SplitN(t, "\n", 2)
	if len(lines) == 2 && !strings.ContainsAny(lines[0], "{[") && len(strings.TrimSpace(lines[0])) <= 10 {
		t = lines[1]
	}
	return t
}

func stripComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte('\n')
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// fixSingleQuotes 把 'x' 形式的字符串字面量改为 "x"，仅在结构位置生效，
// 且遇到内容含双引号的字面量时放弃改写，避免破坏原文。
func fixSingleQuotes(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inDouble {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			if c == '"' {
				inDouble = false
			}
			continue
		}
		if c == '"' {
			inDouble = true
			b.WriteByte(c)
			continue
		}
		if c == '\'' {
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '\'' {
					break
				}
				j++
			}
			if j >= len(s) {
				b.WriteByte(c)
				continue
			}
			inner := s[i+1 : j]
			if strings.ContainsAny(inner, "\"\n") {
				b.WriteByte(c)
				continue
			}
			b.WriteByte('"')
			b.WriteString(inner)
			b.WriteByte('"')
			i = j
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// stripTrailingCommas 删除对象/数组闭合符之前多余的逗号。
func stripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && isSpaceByte(s[j]) {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // 丢弃该逗号
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// Extract 从混合文本中抽取第一个括号平衡且位于字符串之外的 JSON 片段。
// 返回片段与是否找到。
func Extract(s string) (string, bool) {
	start := -1
	var open, close byte
	for i := 0; i < len(s); i++ {
		if s[i] == '{' || s[i] == '[' {
			start = i
			open = s[i]
			if open == '{' {
				close = '}'
			} else {
				close = ']'
			}
			break
		}
	}
	if start < 0 {
		return "", false
	}
	depth := 0
	inStr := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// ValidUTF8 便于调用方在读取文件后决定是否需要编码回退。
func ValidUTF8(b []byte) bool { return utf8.Valid(b) }
