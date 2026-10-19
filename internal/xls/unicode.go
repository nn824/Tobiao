package xls

import "unicode/utf16"

// encodeUTF16 把字符串编码为 UTF-16LE 字节序列（BIFF8 的字符存储格式）。
func encodeUTF16(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		out[i*2] = byte(u)
		out[i*2+1] = byte(u >> 8)
	}
	return out
}
