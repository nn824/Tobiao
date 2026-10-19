// 本文件实现 .xls（OLE2 复合文档 + BIFF8）的只读解析，
// 用于把已经生成的计算书读回来做比对。与写出端一样不依赖任何第三方库。
package xls

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"unicode/utf16"
)

// 读取端额外的 BIFF8 记录号（写出端用不到的都放这里）。
const (
	recFormula = 0x0006
	recRString = 0x00D6
	recMulRK   = 0x00BD
	recBoolErr = 0x0205
	recLabel   = 0x0204
	recRK      = 0x027E
	recString  = 0x0207
)

// cfbMagic 是 OLE2 复合文档的文件头签名。
var cfbMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// Book 是从 .xls 读出的工作簿，工作表顺序与 BOUNDSHEET 一致。
type Book struct {
	Sheets []*Sheet
}

// SheetByName 按名字查找工作表（忽略大小写与首尾空格），找不到返回 nil。
func (b *Book) SheetByName(name string) *Sheet {
	if b == nil {
		return nil
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, s := range b.Sheets {
		if strings.ToLower(strings.TrimSpace(s.Name)) == want {
			return s
		}
	}
	return nil
}

// ReadFile 读取 .xls 文件并解析出全部工作表。
func ReadFile(path string) (*Book, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("xls: 读取 %s 失败: %w", path, err)
	}
	return ReadBytes(data)
}

// ReadBytes 解析 .xls 字节：OLE2 容器会自动解封，裸 BIFF 流也能直接读。
func ReadBytes(data []byte) (*Book, error) {
	if len(data) < 8 {
		return nil, errors.New("xls: 文件太小，不是有效的 .xls")
	}
	if bytes.HasPrefix(data, cfbMagic) {
		stream, err := ReadCFB(data, "Workbook")
		if err != nil {
			return nil, err
		}
		data = stream
	}
	return ReadWorkbook(data)
}

// ---------------------------------------------------------------------------
// OLE2 复合文档
// ---------------------------------------------------------------------------

// ReadCFB 从 OLE2 复合文档中读出名为 streamName 的流。
// streamName 留空时取 "Workbook"（Excel 97-2003 工作簿流的固定名字）。
func ReadCFB(data []byte, streamName string) ([]byte, error) {
	if streamName == "" {
		streamName = "Workbook"
	}
	if len(data) < 512 || !bytes.HasPrefix(data, cfbMagic) {
		return nil, errors.New("xls: 不是 OLE2 复合文档（.xls）")
	}

	shift := binary.LittleEndian.Uint16(data[30:]) // 扇区大小 = 1 << shift
	if shift < 7 || shift > 20 {
		shift = 9
	}
	sectSize := 1 << shift
	miniCutoff := binary.LittleEndian.Uint32(data[56:])
	numFAT := binary.LittleEndian.Uint32(data[44:])
	firstDir := binary.LittleEndian.Uint32(data[48:])
	firstDIFAT := binary.LittleEndian.Uint32(data[68:])
	numDIFAT := binary.LittleEndian.Uint32(data[72:])

	// 1) 收集 FAT 扇区号：前 109 项在文件头，其余在 DIFAT 追加扇区里。
	fatSects := make([]uint32, 0, numFAT)
	for i := 0; i < 109 && uint32(len(fatSects)) < numFAT; i++ {
		fatSects = append(fatSects, binary.LittleEndian.Uint32(data[76+i*4:]))
	}
	next := firstDIFAT
	for i := uint32(0); i < numDIFAT && uint32(len(fatSects)) < numFAT; i++ {
		if next == sidEndOfChain || next == sidFree || next == sidUsedBySAT || next == sidUsedByDIFAT {
			break
		}
		off := sectorOffset(next, sectSize)
		if off+sectSize > len(data) {
			break
		}
		per := sectSize/4 - 1 // 末 4 字节是下一个 DIFAT 扇区号
		for j := 0; j < per && uint32(len(fatSects)) < numFAT; j++ {
			fatSects = append(fatSects, binary.LittleEndian.Uint32(data[off+j*4:]))
		}
		next = binary.LittleEndian.Uint32(data[off+per*4:])
	}

	// 2) 展开 FAT。
	fat := make([]uint32, 0, len(fatSects)*(sectSize/4))
	for _, s := range fatSects {
		off := sectorOffset(s, sectSize)
		if off+sectSize > len(data) {
			return nil, fmt.Errorf("xls: FAT 扇区 %d 越界，文件可能已损坏", s)
		}
		for p := 0; p+4 <= sectSize; p += 4 {
			fat = append(fat, binary.LittleEndian.Uint32(data[off+p:]))
		}
	}
	nextSector := func(s uint32) (uint32, bool) {
		if int(s) >= len(fat) {
			return 0, false
		}
		return fat[s], true
	}

	// 3) 遍历目录：找到目标流，同时记下 Root Entry（迷你流的宿主）。
	dir, err := readChain(data, nextSector, firstDir, sectSize, 0)
	if err != nil {
		return nil, fmt.Errorf("xls: 读取目录失败: %w", err)
	}
	var (
		names       []string
		targetName  string
		targetStart uint32
		targetSize  int64
		rootStart   uint32
		rootSize    int64
		hasTarget   bool
	)
	for off := 0; off+cfbDirEntry <= len(dir); off += cfbDirEntry {
		e := dir[off : off+cfbDirEntry]
		switch e[66] {
		case 0x05: // Root Entry：迷你流的起点与总长度
			rootStart = binary.LittleEndian.Uint32(e[116:])
			rootSize = int64(binary.LittleEndian.Uint64(e[120:]))
		case 0x02: // 流对象
			name := dirEntryName(e)
			names = append(names, name)
			if !hasTarget && strings.EqualFold(name, streamName) {
				hasTarget = true
				targetName = name
				targetStart = binary.LittleEndian.Uint32(e[116:])
				targetSize = int64(binary.LittleEndian.Uint64(e[120:]))
			}
		}
	}
	if !hasTarget {
		return nil, fmt.Errorf("xls: 复合文档里没有名为 %q 的流（找到：%s）",
			streamName, strings.Join(names, "、"))
	}

	// 4) 小于迷你流阈值的流按规范放在 Root Entry 的迷你流里。
	//    本工具的写出器不产生迷你流（文件头把迷你 FAT 标成 EOC），
	//    这种文件仍按常规 FAT 链读，两种形态都要能读。
	miniFATStart := binary.LittleEndian.Uint32(data[60:])
	miniFATCount := binary.LittleEndian.Uint32(data[64:])
	if targetSize < int64(miniCutoff) && miniFATCount > 0 && miniFATStart != sidEndOfChain {
		miniFatRaw, err := readChain(data, nextSector, miniFATStart, sectSize, int(miniFATCount)*sectSize)
		if err != nil {
			return nil, fmt.Errorf("xls: 读取迷你 FAT 失败: %w", err)
		}
		miniFat := make([]uint32, len(miniFatRaw)/4)
		for i := range miniFat {
			miniFat[i] = binary.LittleEndian.Uint32(miniFatRaw[i*4:])
		}
		miniSectSize := 1 << binary.LittleEndian.Uint16(data[32:])
		if miniSectSize <= 0 || miniSectSize > sectSize {
			miniSectSize = 64
		}
		raw, err := readMiniStream(data, nextSector, sectSize, rootStart, int(rootSize),
			miniFat, miniSectSize, targetStart, int(targetSize))
		if err != nil {
			return nil, fmt.Errorf("xls: 读取流 %q 失败: %w", targetName, err)
		}
		return raw, nil
	}

	raw, err := readChain(data, nextSector, targetStart, sectSize, int(targetSize))
	if err != nil {
		return nil, fmt.Errorf("xls: 读取流 %q 失败: %w", targetName, err)
	}
	if int64(len(raw)) < targetSize {
		return nil, fmt.Errorf("xls: 流 %q 只有 %d 字节，少于声明的 %d 字节", targetName, len(raw), targetSize)
	}
	return raw[:int(targetSize)], nil
}

// readMiniStream 从迷你流（Root Entry 描述的那一段）里按迷你 FAT 链取出目标流。
func readMiniStream(data []byte, next func(uint32) (uint32, bool), sectSize int,
	rootStart uint32, rootSize int, miniFat []uint32, miniSectSize int,
	start uint32, size int) ([]byte, error) {
	if rootSize <= 0 {
		return nil, errors.New("迷你流的 Root Entry 长度为 0")
	}
	mini, err := readChain(data, next, rootStart, sectSize, rootSize)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, size)
	s := start
	for len(out) < size {
		if s == sidEndOfChain || s >= 0xFFFFFFF0 || int(s) >= len(miniFat) {
			break
		}
		off := int(s) * miniSectSize
		if off+miniSectSize > len(mini) {
			return nil, fmt.Errorf("迷你扇区 %d 越过迷你流末尾", s)
		}
		out = append(out, mini[off:off+miniSectSize]...)
		s = miniFat[s]
	}
	if len(out) < size {
		return nil, fmt.Errorf("迷你流数据不足：只有 %d 字节，需要 %d 字节", len(out), size)
	}
	return out[:size], nil
}

// readChain 沿 FAT 链读出一个流；limit <= 0 表示一直读到链尾。
func readChain(data []byte, next func(uint32) (uint32, bool), start uint32, sectSize, limit int) ([]byte, error) {
	out := make([]byte, 0, 4096)
	s := start
	seen := map[uint32]bool{}
	for {
		if s == sidEndOfChain || s >= 0xFFFFFFF0 {
			break
		}
		if seen[s] {
			return nil, errors.New("FAT 链成环")
		}
		seen[s] = true
		off := sectorOffset(s, sectSize)
		if off+sectSize > len(data) {
			return nil, fmt.Errorf("扇区 %d 越过文件末尾", s)
		}
		out = append(out, data[off:off+sectSize]...)
		if limit > 0 && len(out) >= limit {
			return out[:limit], nil
		}
		nx, ok := next(s)
		if !ok {
			return nil, fmt.Errorf("FAT 链在扇区 %d 处越界", s)
		}
		s = nx
	}
	return out, nil
}

func sectorOffset(sect uint32, sectSize int) int {
	return (int(sect) + 1) * sectSize
}

// dirEntryName 取目录项名字（UTF-16LE，去掉结尾的 NUL）。
func dirEntryName(e []byte) string {
	n := int(binary.LittleEndian.Uint16(e[64:]))
	if n > 64 {
		n = 64
	}
	n &^= 1
	units := make([]uint16, 0, n/2)
	for i := 0; i+1 < n; i += 2 {
		units = append(units, binary.LittleEndian.Uint16(e[i:]))
	}
	return strings.TrimRight(string(utf16.Decode(units)), "\x00")
}

// ---------------------------------------------------------------------------
// BIFF8 记录
// ---------------------------------------------------------------------------

// ReadWorkbook 解析 Workbook 流（一个全局子流 + 若干工作表子流）。
func ReadWorkbook(stream []byte) (*Book, error) {
	if len(stream) < 4 {
		return nil, errors.New("xls: 工作簿流为空")
	}
	p := &biffParser{sst: &sstBuilder{}}
	for i := 0; i+4 <= len(stream); {
		id := binary.LittleEndian.Uint16(stream[i:])
		size := int(binary.LittleEndian.Uint16(stream[i+2:]))
		if size < 0 || i+4+size > len(stream) {
			break
		}
		p.handle(id, stream[i+4:i+4+size])
		i += 4 + size
	}
	if len(p.sheets) == 0 {
		return nil, errors.New("xls: 工作簿里没有工作表")
	}
	// BOUNDSHEET 与工作表子流一一对应，按顺序补上名字；名字不够时用默认名。
	for i, s := range p.sheets {
		if i < len(p.names) && p.names[i] != "" {
			s.Name = p.names[i]
		} else if s.Name == "" {
			s.Name = fmt.Sprintf("Sheet%d", i+1)
		}
	}
	return &Book{Sheets: p.sheets}, nil
}

type biffParser struct {
	sst      *sstBuilder
	inSST    bool
	names    []string
	sheets   []*Sheet
	cur      *Sheet
	pendRow  int // 待由 STRING 记录补全的公式单元格
	pendCol  int
	pendFmla bool
}

func (p *biffParser) handle(id uint16, b []byte) {
	switch id {
	case recBOF:
		p.inSST = false
		if len(b) >= 4 && binary.LittleEndian.Uint16(b[2:]) == bofWorksheet {
			p.cur = &Sheet{}
			p.sheets = append(p.sheets, p.cur)
		} else {
			p.cur = nil
		}
	case recEOF:
		p.cur = nil
		p.inSST = false
	case recSST:
		p.inSST = true
		if len(b) >= 8 {
			p.sst.addChunk(b[8:], false)
		}
	case recContinue:
		if p.inSST {
			p.sst.addChunk(b, true)
		}
	case recBoundSheet:
		p.names = append(p.names, boundSheetName(b))
	case recDimensions:
		// 只用于预分配，可忽略
	case recRow:
		if p.cur != nil && len(b) >= 8 {
			row := int(binary.LittleEndian.Uint16(b[0:]))
			h := int(binary.LittleEndian.Uint16(b[6:]))
			if h&0x8000 == 0 { // bit15 置位表示用默认行高
				p.cur.SetHeight(row, h)
			}
		}
	case recMergeCells:
		if p.cur != nil && len(b) >= 2 {
			n := int(binary.LittleEndian.Uint16(b[0:]))
			for k := 0; k < n && 2+k*8+8 <= len(b); k++ {
				o := 2 + k*8
				p.cur.AddMerge(
					int(binary.LittleEndian.Uint16(b[o:])),
					int(binary.LittleEndian.Uint16(b[o+2:])),
					int(binary.LittleEndian.Uint16(b[o+4:])),
					int(binary.LittleEndian.Uint16(b[o+6:])),
				)
			}
		}
	case recLabelSST:
		if p.cur != nil && len(b) >= 10 {
			p.text(int(binary.LittleEndian.Uint16(b[0:])), int(binary.LittleEndian.Uint16(b[2:])),
				int(binary.LittleEndian.Uint16(b[4:])), p.sst.get(binary.LittleEndian.Uint32(b[6:])))
		}
	case recLabel, recRString:
		if p.cur != nil && len(b) >= 6 {
			s, err := decodeXLUnicodeString(b[6:])
			if err == nil {
				p.text(int(binary.LittleEndian.Uint16(b[0:])), int(binary.LittleEndian.Uint16(b[2:])),
					int(binary.LittleEndian.Uint16(b[4:])), s)
			}
		}
	case recNumber:
		if p.cur != nil && len(b) >= 14 {
			p.num(int(binary.LittleEndian.Uint16(b[0:])), int(binary.LittleEndian.Uint16(b[2:])),
				int(binary.LittleEndian.Uint16(b[4:])), math.Float64frombits(binary.LittleEndian.Uint64(b[6:])))
		}
	case recRK:
		if p.cur != nil && len(b) >= 10 {
			p.num(int(binary.LittleEndian.Uint16(b[0:])), int(binary.LittleEndian.Uint16(b[2:])),
				int(binary.LittleEndian.Uint16(b[4:])), decodeRK(binary.LittleEndian.Uint32(b[6:])))
		}
	case recMulRK:
		if p.cur != nil && len(b) >= 6 {
			row := int(binary.LittleEndian.Uint16(b[0:]))
			col := int(binary.LittleEndian.Uint16(b[2:]))
			for o := 4; o+6 <= len(b)-2; o += 6 {
				p.num(row, col, int(binary.LittleEndian.Uint16(b[o:])),
					decodeRK(binary.LittleEndian.Uint32(b[o+2:])))
				col++
			}
		}
	case recBoolErr:
		if p.cur != nil && len(b) >= 8 {
			v := "FALSE"
			if b[6] != 0 {
				v = "TRUE"
			}
			if b[7] != 0 {
				v = "#ERR"
			}
			p.text(int(binary.LittleEndian.Uint16(b[0:])), int(binary.LittleEndian.Uint16(b[2:])),
				int(binary.LittleEndian.Uint16(b[4:])), v)
		}
	case recFormula:
		if p.cur != nil && len(b) >= 14 {
			row := int(binary.LittleEndian.Uint16(b[0:]))
			col := int(binary.LittleEndian.Uint16(b[2:]))
			xf := int(binary.LittleEndian.Uint16(b[4:]))
			if binary.LittleEndian.Uint16(b[12:]) == 0xFFFF {
				// 结果是字符串，真实值由紧随其后的 STRING 记录给出。
				p.pendFmla, p.pendRow, p.pendCol = true, row, col
				p.cur.put(row, col, Cell{kind: cellText, style: xf})
				return
			}
			p.num(row, col, xf, math.Float64frombits(binary.LittleEndian.Uint64(b[6:])))
		}
	case recString:
		if p.cur != nil && p.pendFmla {
			p.pendFmla = false
			if s, err := decodeXLUnicodeString(b); err == nil {
				p.cur.put(p.pendRow, p.pendCol, Cell{kind: cellText, text: s, style: p.cur.RowStyle(p.pendRow, p.pendCol)})
			}
		}
	case recBlank:
		if p.cur != nil && len(b) >= 6 {
			p.cur.put(int(binary.LittleEndian.Uint16(b[0:])), int(binary.LittleEndian.Uint16(b[2:])),
				Cell{kind: cellBlank, style: int(binary.LittleEndian.Uint16(b[4:]))})
		}
	}
}

func (p *biffParser) text(r, c, xf int, v string) {
	if p.cur == nil {
		return
	}
	if v == "" {
		p.cur.put(r, c, Cell{kind: cellBlank, style: xf})
		return
	}
	p.cur.put(r, c, Cell{kind: cellText, text: v, style: xf})
}

func (p *biffParser) num(r, c, xf int, v float64) {
	if p.cur == nil {
		return
	}
	p.cur.put(r, c, Cell{kind: cellNumber, num: v, style: xf})
}

// RowStyle 返回已存在单元格的 XF 索引，不存在时返回 0；仅用于补全公式字符串单元格。
func (s *Sheet) RowStyle(r, c int) int {
	if cell, ok := s.CellAt(r, c); ok {
		return cell.Style()
	}
	return 0
}

// boundSheetName 取 BOUNDSHEET 里的工作表名。
func boundSheetName(b []byte) string {
	if len(b) < 9 {
		return ""
	}
	cch := int(b[6])
	wide := b[7]&0x01 != 0
	raw := b[8:]
	if wide {
		if len(raw) < cch*2 {
			cch = len(raw) / 2
		}
		units := make([]uint16, 0, cch)
		for i := 0; i < cch*2; i += 2 {
			units = append(units, binary.LittleEndian.Uint16(raw[i:]))
		}
		return string(utf16.Decode(units))
	}
	if len(raw) < cch {
		cch = len(raw)
	}
	return string(raw[:cch])
}

// decodeRK 解出 RK 记录里的数值。
func decodeRK(v uint32) float64 {
	val := v & 0xFFFFFFFC
	var f float64
	if v&0x02 != 0 { // fInt：30 位有符号整数
		n := int32(val)
		if val&0x20000000 != 0 {
			n -= 0x40000000 // 30 位补码的符号扩展
		}
		f = float64(n)
	} else { // 浮点：低 34 位补 0
		f = math.Float64frombits(uint64(val) << 32)
	}
	if v&0x01 != 0 { // fX100
		f /= 100
	}
	return f
}

// decodeXLUnicodeString 解析 BIFF8 的 XLUnicodeString（2 字节长度 + 1 字节选项 + 字符）。
// 富文本 / 注音部分会被跳过，只返回纯文本。
func decodeXLUnicodeString(b []byte) (string, error) {
	if len(b) < 3 {
		return "", errors.New("字符串记录过短")
	}
	cch := int(binary.LittleEndian.Uint16(b[0:]))
	flags := b[2]
	i := 3
	unit := 1
	if flags&0x01 != 0 {
		unit = 2
	}
	if flags&0x08 != 0 { // fRichStr：跳过 cRun
		i += 2
	}
	if flags&0x04 != 0 { // fExtSt：跳过 cbExtRst 与其数据
		if i+4 > len(b) {
			return "", errors.New("注音字段越界")
		}
		n := int(binary.LittleEndian.Uint32(b[i:]))
		i += 4 + n
	}
	if i+cch*unit > len(b) {
		cch = (len(b) - i) / unit
	}
	units := make([]uint16, 0, cch)
	for k := 0; k < cch; k++ {
		if unit == 2 {
			units = append(units, binary.LittleEndian.Uint16(b[i+k*2:]))
		} else {
			units = append(units, uint16(b[i+k]))
		}
	}
	return string(utf16.Decode(units)), nil
}

// ---------------------------------------------------------------------------
// SST（共享字符串表）
// ---------------------------------------------------------------------------

// sstBuilder 增量拼接 SST 与其 CONTINUE 记录里的字符串。
type sstBuilder struct {
	out  []string
	buf  []uint16
	rem  int  // 当前字符串还差几个字符
	wide bool // 当前字符串是否 UTF-16LE
}

// addChunk 喂入一段数据；cont 为 true 时表示来自 CONTINUE 记录，
// 此时若上一个字符串还没读完，首字节是新的压缩标志。
func (s *sstBuilder) addChunk(b []byte, cont bool) {
	i := 0
	if cont && s.rem > 0 && len(b) > 0 {
		s.wide = b[0]&0x01 != 0
		i = 1
	}
	for i < len(b) {
		if s.rem == 0 {
			// 开始一个新字符串：需要 cch(2) + flags(1)。
			if len(b)-i < 3 {
				return
			}
			cch := int(binary.LittleEndian.Uint16(b[i:]))
			flags := b[i+2]
			i += 3
			if flags&0x08 != 0 {
				if len(b)-i < 2 {
					return
				}
				i += 2 // cRun
			}
			if flags&0x04 != 0 {
				if len(b)-i < 4 {
					return
				}
				n := int(binary.LittleEndian.Uint32(b[i:]))
				i += 4 + n // cbExtRst + 注音数据
			}
			s.wide = flags&0x01 != 0
			s.buf = s.buf[:0]
			s.rem = cch
			if cch == 0 {
				s.out = append(s.out, "")
				continue
			}
		}
		unit := 1
		if s.wide {
			unit = 2
		}
		for s.rem > 0 && i+unit <= len(b) {
			if s.wide {
				s.buf = append(s.buf, binary.LittleEndian.Uint16(b[i:]))
			} else {
				s.buf = append(s.buf, uint16(b[i]))
			}
			i += unit
			s.rem--
		}
		if s.rem == 0 {
			s.out = append(s.out, string(utf16.Decode(s.buf)))
			s.buf = s.buf[:0]
		}
		if s.rem > 0 { // 半个字符，留给下一段
			return
		}
	}
}

// get 返回第 idx 个共享字符串；越界时返回空串而不是 panic。
func (s *sstBuilder) get(idx uint32) string {
	if int(idx) < len(s.out) {
		return s.out[idx]
	}
	return ""
}
