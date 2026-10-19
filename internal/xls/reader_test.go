package xls

import (
	"bytes"
	"strings"
	"testing"
)

// TestReadWriteRoundTrip 保证「写出去的能原样读回来」：
// 比对功能完全依赖读取端，这条链路断了整个功能就没意义。
func TestReadWriteRoundTrip(t *testing.T) {
	sheet := &Sheet{Name: "测试表", ColWidths: []int{2000, 9000}}
	wb := NewWorkbook(sheet)

	sheet.SetText(0, 0, "序号", XfTableHead)
	sheet.SetText(0, 1, "工程量计算式", XfTableHead)
	sheet.SetNumber(1, 0, 1, XfTableCenter)
	sheet.SetText(1, 1, "周长 = 1.6m\n体积 = 0.72m3", XfTableCell)
	sheet.SetNumber(2, 0, 2.5, XfTableCenter)
	sheet.SetText(2, 1, "KZ9 中文 + ASCII", XfTableCell)

	// 超过 8224 字节的长字符串会跨 SST / CONTINUE 记录，专门覆盖这条路径。
	long := strings.Repeat("超长字符串用于触发共享字符串表的 CONTINUE 记录。", 400)
	sheet.SetText(3, 1, long, XfTableCell)

	sheet.AddMerge(1, 2, 0, 0)
	sheet.SetHeight(0, 300)

	stream, err := wb.Write()
	if err != nil {
		t.Fatalf("生成工作簿失败: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteCFB(&buf, "Workbook", stream); err != nil {
		t.Fatalf("封装 OLE2 失败: %v", err)
	}

	book, err := ReadBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("读回 .xls 失败: %v", err)
	}
	if len(book.Sheets) != 1 {
		t.Fatalf("应当读到 1 张工作表，实际 %d", len(book.Sheets))
	}
	s := book.Sheets[0]
	if s.Name != "测试表" {
		t.Errorf("工作表名应为「测试表」，实际 %q", s.Name)
	}

	cases := []struct {
		r, c int
		want string
	}{
		{0, 0, "序号"},
		{0, 1, "工程量计算式"},
		{1, 0, "1"},
		{1, 1, "周长 = 1.6m\n体积 = 0.72m3"},
		{2, 0, "2.5"},
		{2, 1, "KZ9 中文 + ASCII"},
		{3, 1, long},
	}
	for _, tc := range cases {
		cell, ok := s.CellAt(tc.r, tc.c)
		if !ok {
			t.Errorf("单元格 (%d,%d) 不存在", tc.r, tc.c)
			continue
		}
		if got := cell.Value(); got != tc.want {
			t.Errorf("单元格 (%d,%d) 应为 %q，实际 %q", tc.r, tc.c, tc.want, got)
		}
	}

	if len(s.Merges) != 1 || s.Merges[0] != (Merge{RowFirst: 1, RowLast: 2, ColFirst: 0, ColLast: 0}) {
		t.Errorf("合并区域读回不正确: %+v", s.Merges)
	}
	if h := s.Height(0); h != 300 {
		t.Errorf("行高应为 300，实际 %d", h)
	}
}

// TestReadBytesRejectsGarbage 确保脏文件给出错误而不是 panic。
func TestReadBytesRejectsGarbage(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("not an xls"),
		[]byte(strings.Repeat("\x00", 600)),
	}
	for _, c := range cases {
		if _, err := ReadBytes(c); err == nil {
			t.Errorf("输入 %d 字节的脏数据时应当报错", len(c))
		}
	}
}

// TestReadCFBMissingStream 覆盖「容器里没有目标流」的错误分支。
func TestReadCFBMissingStream(t *testing.T) {
	sheet := &Sheet{Name: "s"}
	wb := NewWorkbook(sheet)
	sheet.SetText(0, 0, "x", XfTableCell)
	if _, err := wb.Write(); err != nil {
		t.Fatalf("生成工作簿失败: %v", err)
	}
	stream, _ := wb.Write()
	var buf bytes.Buffer
	if err := WriteCFB(&buf, "Workbook", stream); err != nil {
		t.Fatalf("封装失败: %v", err)
	}
	if _, err := ReadCFB(buf.Bytes(), "NoSuchStream"); err == nil {
		t.Fatal("找不到流时应当报错")
	}
}
