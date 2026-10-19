// Package render 实现「工程量计算式」的文本渲染与行高估算。
//
// 渲染规则（由真实导出文件 新.xls 反推）：
//  1. 每行形如「显示名 = 计算式 = 计算结果单位」，多行以换行符连接；
//  2. 计算式中的【名称】统一改写为 <名称>；
//  3. 计算式中所有数字按「四舍五入到 4 位小数后去掉多余的 0」规范化；
//  4. 计算结果由 JSL（以毫米为基准的整数）除以量纲除数后同样规范化，
//     单位由 ³ 改写成 3，² 改写成 2；
//  5. 当计算式为空时，左式退化为「数值<原始量标签>」。
package render

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/ninglang/zhiduoxing-xls/internal/spec"
)

// 行列布局常量（与真实文件一致）
const (
	// 行高：单行 285 twips，每多一行增加 210 twips。
	RowHeightBase = 285
	RowHeightStep = 210
	// 计算式列的自动换行容量，单位「半角字符宽度」。
	CellCapacity = 79
)

// RoundDigits 是数值规范化保留的小数位数。
const RoundDigits = 4

var numberToken = regexp.MustCompile(`\d+(?:\.\d+)?`)

// normalizeScaled 把 scaled/scale 精确地四舍五入到 RoundDigits 位小数，
// 去掉尾随的 0 与小数点。舍入方式为「四舍五入（远离零）」。
func normalizeScaled(scaled *big.Int, div int64) string {
	num := new(big.Int).Mul(scaled, big.NewInt(pow10(RoundDigits)))
	d := big.NewInt(div)
	q, r := new(big.Int).QuoRem(num, d, new(big.Int))

	half := new(big.Int).Abs(r)
	half.Lsh(half, 1)
	absD := new(big.Int).Abs(d)
	if half.Cmp(absD) >= 0 {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}

	neg := q.Sign() < 0
	q.Abs(q)
	s := q.String()
	for len(s) <= RoundDigits {
		s = "0" + s
	}
	intPart := s[:len(s)-RoundDigits]
	frac := strings.TrimRight(s[len(s)-RoundDigits:], "0")
	out := intPart
	if frac != "" {
		out += "." + frac
	}
	if neg && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out
}

func pow10(n int) int64 {
	v := int64(1)
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}

// NormalizeLiteral 规范化一个十进制字面量（保留原符号）。
func NormalizeLiteral(tok string) string {
	r, ok := new(big.Rat).SetString(tok)
	if !ok {
		return tok
	}
	return normalizeScaled(r.Num(), r.Denom().Int64()*1)
}

// normalizeRational 规范化一个有理数。
func normalizeRational(r *big.Rat) string {
	return normalizeScaled(r.Num(), r.Denom().Int64())
}

// Value 把工程量数值（JSL）按量纲还原为规范化字符串。
func Value(jsl int64, unit spec.Unit) string {
	return normalizeScaled(big.NewInt(jsl), spec.Divisor(unit))
}

// ValueFromString 解析 JSL 字符串并按量纲还原；无法解析时返回空串。
func ValueFromString(jsl string, unit spec.Unit) (string, bool) {
	t := strings.TrimSpace(jsl)
	if t == "" {
		return "", false
	}
	v, ok := new(big.Int).SetString(t, 10)
	if !ok {
		return "", false
	}
	return normalizeScaled(v, spec.Divisor(unit)), true
}

// Formula 渲染计算式：替换书名号标记并规范化其中的数字。
func Formula(jss string) string {
	if jss == "" {
		return ""
	}
	s := strings.ReplaceAll(jss, "【", "<")
	s = strings.ReplaceAll(s, "】", ">")
	s = numberToken.ReplaceAllStringFunc(s, NormalizeLiteral)
	return s
}

// Result 渲染计算结果，把 ³/² 改写成 3/2 并去掉前导的等号。
func Result(jssjg string) string {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(jssjg), "="))
	s = strings.ReplaceAll(s, "³", "3")
	s = strings.ReplaceAll(s, "²", "2")
	s = strings.ReplaceAll(s, "㎡", "m2")
	s = strings.ReplaceAll(s, "㎥", "m3")
	return s
}

// Line 渲染一行工程量。
//
//	name      显示名
//	raw       原始量标签（计算式为空时使用）
//	formula   计算式（已或未经 Formula 处理均可，此处会再规范化一次）
//	jsl       工程量数值字符串
//	unit      量纲
//	suffix    结果单位文本
//	fallback  当数值无法解析或为空时使用的计算结果文本
func Line(name, raw, formula, jsl string, unit spec.Unit, suffix, fallback string) string {
	lhs := Formula(formula)
	if lhs == "" {
		if raw == "" {
			raw = name
		}
		if v, ok := ValueFromString(jsl, unit); ok {
			lhs = v + "<" + raw + ">"
		} else {
			lhs = "<" + raw + ">"
		}
	}
	rhs := ""
	if v, ok := ValueFromString(jsl, unit); ok {
		rhs = v + suffix
	} else {
		rhs = Result(fallback)
	}
	return name + " = " + lhs + " = " + rhs
}

// QuantityLine 渲染「数量」行。数量固定为 1（图元即单个构件），单位为根/樘/个/块。
func QuantityLine(unit string) string {
	if unit == "" {
		return ""
	}
	return "数量 = 1 = 1" + unit
}

// VisualWidth 估算字符串在表格中的显示宽度：CJK 等全角字符记 2，其余记 1。
func VisualWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x2E80 {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// DisplayLines 估算文本在给定列宽容量下自动换行后占用的显示行数。
func DisplayLines(text string, capacity int) int {
	if capacity <= 0 {
		capacity = CellCapacity
	}
	if text == "" {
		return 1
	}
	total := 0
	for _, ln := range strings.Split(text, "\n") {
		w := VisualWidth(ln)
		n := (w + capacity - 1) / capacity
		if n < 1 {
			n = 1
		}
		total += n
	}
	if total < 1 {
		total = 1
	}
	return total
}

// RowHeight 由显示行数推算行高（twips）。
func RowHeight(displayLines int) int {
	if displayLines < 1 {
		displayLines = 1
	}
	return RowHeightBase + (displayLines-1)*RowHeightStep
}
