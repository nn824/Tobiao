package xls

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"unicode/utf16"
)

// sampleWorkbook 构造一个覆盖全部样式与单元格类型的小工作簿。
func sampleWorkbook() *Workbook {
	sheet := &Sheet{Name: "绘图输入构件工程量计算书-首层", ColWidths: []int{1865, 5522, 17518, 17518}}
	wb := NewWorkbook(sheet)
	sheet.SetText(0, 0, "序号", XfHeaderLeft)
	sheet.SetText(0, 1, "图元位置", XfHeaderLeft)
	sheet.SetText(0, 2, "工程量计算式", XfHeaderRight)
	sheet.SetHeight(0, 285)

	title := "首层 - 柱 - KZ9"
	for c := 0; c < 3; c++ {
		style := XfGroupLeft
		if c == 2 {
			style = XfGroupRight
		}
		sheet.SetText(1, c, title, style)
	}
	sheet.SetHeight(1, 285)
	sheet.AddMerge(1, 1, 0, 2)

	detail := "周长 = ((0.4<长度>+0.4<宽度>)*2) = 1.6m"
	for i := 0; i < 3; i++ {
		nameRow := 2 + i*2
		detailRow := nameRow + 1
		sheet.SetNumber(nameRow, 0, float64(i+1), XfSerial)
		sheet.SetText(nameRow, 1, "<6,D>", XfPosition)
		sheet.SetText(nameRow, 2, "柱: KZ9", XfNameRight)
		sheet.SetNumber(detailRow, 0, float64(i+1), XfSerial)
		sheet.SetText(detailRow, 1, "<6,D>", XfPosition)
		sheet.SetText(detailRow, 2, detail, XfDetailRight)
		sheet.SetHeight(nameRow, 285)
		sheet.SetHeight(detailRow, 1545)
		sheet.AddMerge(nameRow, detailRow, 0, 0)
		sheet.AddMerge(nameRow, detailRow, 1, 1)
	}
	// 中间一组换成收口样式，用于验证 SetStyle 会覆盖既有单元格的样式。
	sheet.SetStyle(5, 0, XfLastSerial)
	sheet.SetStyle(5, 1, XfLastPosition)
	sheet.SetStyle(5, 2, XfLastDetail)
	return wb
}

// --- 极简 OLE2 读取器（仅测试用，独立实现以便真正校验写出结果）---

const (
	tSectorSize = 512
	tEndOfChain = 0xFFFFFFFE
	tFree       = 0xFFFFFFFF
)

type cfbdoc struct {
	raw        []byte
	dirStart   uint32
	fatSectors []uint32
	sat        []uint32
}

func parseCFB(t *testing.T, raw []byte) *cfbdoc {
	t.Helper()
	if len(raw) < tSectorSize {
		t.Fatalf("文件过短: %d 字节", len(raw))
	}
	sig := []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	if !bytes.Equal(raw[:8], sig) {
		t.Fatalf("OLE2 签名不匹配: % x", raw[:8])
	}
	if got := binary.LittleEndian.Uint16(raw[30:]); got != 9 {
		t.Fatalf("扇区大小指数 = %d，期望 9", got)
	}
	fatCount := binary.LittleEndian.Uint32(raw[44:])
	dirStart := binary.LittleEndian.Uint32(raw[48:])

	// DIFAT（文件头内的前 109 项）
	difat := make([]uint32, 0, 109)
	for i := 0; i < 109; i++ {
		v := binary.LittleEndian.Uint32(raw[76+i*4:])
		if v == tFree {
			break
		}
		difat = append(difat, v)
	}
	if uint32(len(difat)) != fatCount {
		t.Fatalf("DIFAT 项数 %d 与头部声明的 FAT 扇区数 %d 不一致", len(difat), fatCount)
	}
	// 拼接 FAT
	sat := make([]uint32, 0, len(difat)*128)
	for _, fs := range difat {
		off := int(tSectorSize) + int(fs)*tSectorSize
		if off+tSectorSize > len(raw) {
			t.Fatalf("FAT 扇区 %d 越界", fs)
		}
		for i := 0; i < 128; i++ {
			sat = append(sat, binary.LittleEndian.Uint32(raw[off+i*4:]))
		}
	}
	return &cfbdoc{raw: raw, dirStart: dirStart, fatSectors: difat, sat: sat}
}

func (d *cfbdoc) chain(start uint32) []uint32 {
	var out []uint32
	s := start
	for s != tEndOfChain && s != tFree && len(out) < len(d.sat) {
		out = append(out, s)
		s = d.sat[s]
	}
	return out
}

// dirEntry 返回目录流的第 i 项。
func (d *cfbdoc) dirEntry(t *testing.T, i int) (name string, start uint32, size uint64) {
	t.Helper()
	off := int(tSectorSize) + int(d.dirStart)*tSectorSize + i*128
	if off+128 > len(d.raw) {
		t.Fatalf("目录项 %d 越界", i)
	}
	e := d.raw[off : off+128]
	n := binary.LittleEndian.Uint16(e[64:])
	if n >= 2 {
		name = string(decodeUTF16(e[:n-2]))
	}
	start = binary.LittleEndian.Uint32(e[116:])
	size = binary.LittleEndian.Uint64(e[120:])
	return
}

// TestCFBStreamChainLength 固化 OLE2 规范中最容易被忽略、且会导致
// xlrd/Excel 报「size exceeds expected N bytes; corrupt?」的那条约束：
// 流的 FAT 链长度必须恰好等于 ceil(流字节数 / 512)，不能多分配扇区。
func TestCFBStreamChainLength(t *testing.T) {
	stream, err := sampleWorkbook().Write()
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteCFB(&buf, "Workbook", stream); err != nil {
		t.Fatalf("WriteCFB: %v", err)
	}
	raw := buf.Bytes()

	if len(raw)%tSectorSize != 0 {
		t.Fatalf("文件长度 %d 不是 512 的整数倍", len(raw))
	}
	d := parseCFB(t, raw)
	name, start, size := d.dirEntry(t, 1)
	if name != "Workbook" {
		t.Fatalf("目录项 1 名称 = %q，期望 Workbook", name)
	}
	if size != uint64(len(stream)) {
		t.Fatalf("目录项声明流大小 %d，实际流大小 %d", size, len(stream))
	}
	chain := d.chain(start)
	want := (len(stream) + tSectorSize - 1) / tSectorSize
	if len(chain) != want {
		t.Fatalf("流 FAT 链长度 %d，期望恰好 %d（多分配会让读取器判定文件损坏）", len(chain), want)
	}

	// 流数据必须原样落在链上的扇区里。
	got := make([]byte, 0, len(stream))
	for _, s := range chain {
		off := int(tSectorSize) + int(s)*tSectorSize
		got = append(got, raw[off:off+tSectorSize]...)
	}
	if !bytes.Equal(got[:len(stream)], stream) {
		t.Fatal("从容器的 FAT 链中读回的字节与写入的流不一致")
	}
}

// TestCFBSectorOwnership 校验各区域扇区互不重叠，且都落在文件范围内。
func TestCFBSectorOwnership(t *testing.T) {
	stream, _ := sampleWorkbook().Write()
	var buf bytes.Buffer
	if err := WriteCFB(&buf, "Workbook", stream); err != nil {
		t.Fatalf("WriteCFB: %v", err)
	}
	d := parseCFB(t, buf.Bytes())
	total := uint32((len(buf.Bytes()) - tSectorSize) / tSectorSize)

	_, bookStart, _ := d.dirEntry(t, 1)
	used := map[uint32]string{}
	claim := func(label string, chain []uint32) {
		for _, s := range chain {
			if s >= total {
				t.Fatalf("%s 引用了越界扇区 %d（共 %d 个扇区）", label, s, total)
			}
			if prev, ok := used[s]; ok {
				t.Fatalf("扇区 %d 被 %s 与 %s 同时占用", s, prev, label)
			}
			used[s] = label
		}
	}
	claim("Workbook", d.chain(bookStart))
	claim("FAT", d.fatSectors)
	claim("目录", d.chain(d.dirStart))

	for _, s := range d.fatSectors {
		if d.sat[s] != 0xFFFFFFFD {
			t.Fatalf("扇区 %d 应被 FAT 标记为 FATSECT(0xFFFFFFFD)，实际 %#x", s, d.sat[s])
		}
	}
}

// --- BIFF 记录层检查 ---

type biffRecord struct {
	id   uint16
	data []byte
}

// decodeUTF16 把 UTF-16LE 字节还原为字符串（测试用，独立实现）。
func decodeUTF16(b []byte) []rune {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return utf16.Decode(u)
}

func readRecords(t *testing.T, b []byte) []biffRecord {
	t.Helper()
	var out []biffRecord
	i := 0
	for i+4 <= len(b) {
		id := binary.LittleEndian.Uint16(b[i:])
		n := int(binary.LittleEndian.Uint16(b[i+2:]))
		if i+4+n > len(b) {
			t.Fatalf("记录 0x%04X 声明长度 %d 越界", id, n)
		}
		out = append(out, biffRecord{id, b[i+4 : i+4+n]})
		i += 4 + n
	}
	return out
}

func TestBiffGlobalsStructure(t *testing.T) {
	stream, err := sampleWorkbook().Write()
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	recs := readRecords(t, stream)
	if len(recs) == 0 || recs[0].id != 0x0809 {
		t.Fatalf("首个记录应为 BOF(0x0809)")
	}
	if got := binary.LittleEndian.Uint16(recs[0].data[2:]); got != 0x0005 {
		t.Fatalf("首个 BOF 类型 = %#x，期望 0x0005（工作簿全局）", got)
	}

	// 统计全局区内的记录
	fonts, written := 0, -1
	sheetName := ""
	for _, r := range recs {
		switch r.id {
		case 0x0031:
			fonts++
		case 0x005C:
			written = len(r.data)
			// WRITEACCESS 必须是规范的 BIFF8 字符串：2 字节字符数 + 1 字节选项 + 字符
			if written < 3 {
				t.Fatalf("WRITEACCESS 过短: %d", written)
			}
			cch := int(binary.LittleEndian.Uint16(r.data[0:]))
			opt := r.data[2]
			need := 3 + cch
			if opt&0x01 != 0 {
				need = 3 + cch*2
			}
			if need > written {
				t.Fatalf("WRITEACCESS 字符串需要 %d 字节，记录只有 %d 字节", need, written)
			}
		case 0x0085:
			n := int(r.data[6])
			raw := r.data[7:]
			if r.data[7]&0x01 != 0 {
				raw = raw[1 : 1+n*2]
				sheetName = string(decodeUTF16(raw))
			} else {
				sheetName = string(raw[1 : 1+n])
			}
		}
	}
	if fonts != 17 {
		t.Errorf("FONT 记录数 = %d，期望 17（BIFF8 的索引 4 是保留占位项，不写出）", fonts)
	}
	if written != 112 {
		t.Errorf("WRITEACCESS 记录长度 = %d，期望 112", written)
	}
	if sheetName != "绘图输入构件工程量计算书-首层" {
		t.Errorf("BOUNDSHEET 工作表名 = %q", sheetName)
	}
}

func TestSSTRoundTrip(t *testing.T) {
	sst := NewSST()
	want := []string{"序号", "图元位置", "工程量计算式", "柱: KZ9", strings.Repeat("长", 300)}
	idx := make([]int, len(want))
	for i, s := range want {
		idx[i] = sst.Add(s)
	}
	if sst.Add(want[0]) != idx[0] {
		t.Error("相同字符串应复用同一个 SST 索引")
	}
	// 每项都要成对出现（重复项共享索引），唯一项为 len(want)。
	if sst.Count() != len(want)+1 || sst.Unique() != len(want) {
		t.Fatalf("SST 计数 = (%d, %d)，期望 (%d, %d)", sst.Count(), sst.Unique(), len(want)+1, len(want))
	}
	got := parseSST(t, sst.Build())
	if len(got) != len(want) {
		t.Fatalf("SST 解析出 %d 项，期望 %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SST[%d] = %q，期望 %q", i, got[i], want[i])
		}
	}
}

// parseSST 解析 SST 记录（含 CONTINUE 分片），验证跨分片的字符串能正确还原。
func parseSST(t *testing.T, b []byte) []string {
	t.Helper()
	// 把 SST 与其后的 CONTINUE 拼成一个「逻辑记录」的选项流：这里逐记录处理，
	// 遇到分片边界时沿用上一个字符串的选项字节。
	blocks := [][]byte{}
	i := 0
	for i+4 <= len(b) {
		if id := binary.LittleEndian.Uint16(b[i:]); id != 0x00FC && id != 0x003C {
			t.Fatalf("SST 分片记录号 = %#x，期望 0x00FC(SST) 或 0x003C(CONTINUE)", id)
		}
		n := int(binary.LittleEndian.Uint16(b[i+2:]))
		if i+4+n > len(b) {
			t.Fatalf("SST 记录越界")
		}
		blocks = append(blocks, b[i+4:i+4+n])
		i += 4 + n
	}
	if len(blocks) == 0 {
		t.Fatal("没有解析到 SST 记录")
	}
	bi, p := 0, 8 // 跳过 SST 头的 8 字节（计数）
	if len(blocks[0]) < 8 {
		t.Fatal("SST 头不足 8 字节")
	}
	var out []string
	for bi < len(blocks) {
		if p >= len(blocks[bi]) {
			bi++
			p = 0
			continue
		}
		if p+3 > len(blocks[bi]) {
			t.Fatal("字符串头跨越记录边界")
		}
		cch := int(binary.LittleEndian.Uint16(blocks[bi][p:]))
		opt := blocks[bi][p+2]
		p += 3
		high := opt&0x01 != 0
		var rs []rune
		for len(rs) < cch {
			if p >= len(blocks[bi]) {
				bi++
				if bi >= len(blocks) {
					t.Fatal("字符串被截断")
				}
				p = 0
				// CONTINUE 开头重复选项字节
				if blocks[bi][0]&0x01 != 0 {
					high = true
				}
				p = 1
				continue
			}
			if high {
				if p+2 > len(blocks[bi]) {
					// 16 位字符不可在中间切断，必须整字符换片
					t.Fatal("UTF-16 字符跨记录边界被切断")
				}
				rs = append(rs, rune(binary.LittleEndian.Uint16(blocks[bi][p:])))
				p += 2
			} else {
				rs = append(rs, rune(blocks[bi][p]))
				p++
			}
		}
		out = append(out, string(rs))
	}
	return out
}

func TestSheetCellKindsAndHeight(t *testing.T) {
	stream, _ := sampleWorkbook().Write()
	recs := readRecords(t, stream)
	// 找到工作表 BOF 之后的记录
	start := -1
	for i, r := range recs {
		if r.id == 0x0809 && binary.LittleEndian.Uint16(r.data[2:]) == 0x0010 {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("未找到工作表 BOF")
	}
	var rowHeights = map[uint16]uint16{}
	var cells []biffRecord
	var merges int
	for _, r := range recs[start:] {
		switch r.id {
		case 0x0208: // ROW
			rowHeights[binary.LittleEndian.Uint16(r.data[0:])] = binary.LittleEndian.Uint16(r.data[6:])
		case 0x0203, 0x027E, 0x00FD: // NUMBER / RK / LABELSST
			cells = append(cells, r)
		case 0x00E5: // MERGEDCELLS
			merges = int(binary.LittleEndian.Uint16(r.data[0:]))
		}
	}
	if rowHeights[0] != 285 || rowHeights[2] != 285 || rowHeights[3] != 1545 {
		t.Errorf("行高不符: %v", rowHeights)
	}
	// 1 个标题行合并 + 3 组「序号/图元位置」竖向合并 × 2。
	if merges != 7 {
		t.Errorf("合并区数量 = %d，期望 7", merges)
	}
	if len(cells) != 24 {
		t.Errorf("单元格记录数 = %d，期望 24", len(cells))
	}
	// 核对样式索引：最后一行应换成收口样式。
	found := map[uint16]bool{}
	for _, c := range cells {
		found[binary.LittleEndian.Uint16(c.data[4:])] = true
	}
	for _, st := range []uint16{XfHeaderLeft, XfHeaderRight, XfGroupLeft, XfGroupRight,
		XfSerial, XfPosition, XfNameRight, XfDetailRight, XfLastSerial, XfLastPosition, XfLastDetail} {
		if !found[st] {
			t.Errorf("样式 XF %d 未被任何单元格引用", st)
		}
	}
}

func TestNumberEncoding(t *testing.T) {
	stream, _ := sampleWorkbook().Write()
	recs := readRecords(t, stream)
	for _, r := range recs {
		if r.id != 0x0203 {
			continue
		}
		got := math.Float64frombits(binary.LittleEndian.Uint64(r.data[6:]))
		if got != 1 {
			t.Fatalf("NUMBER 值 = %v，期望 1", got)
		}
		return
	}
	t.Fatal("未找到 NUMBER 记录")
}
