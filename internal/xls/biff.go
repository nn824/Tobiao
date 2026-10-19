package xls

import (
	"encoding/binary"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
)

// BIFF8 记录号
const (
	recEOF            = 0x000A
	recCalcCount      = 0x000C
	recCalcMode       = 0x000D
	recPrecision      = 0x000E
	recDelta          = 0x0010
	recIteration      = 0x0011
	recProtect        = 0x0012
	recPassword       = 0x0013
	recHeader         = 0x0014
	recFooter         = 0x0015
	recObjectProtect  = 0x0063
	recWindowProtect  = 0x0019
	recScenProtect    = 0x00DD
	recDate1904       = 0x0022
	recLeftMargin     = 0x0026
	recRightMargin    = 0x0027
	recTopMargin      = 0x0028
	recBottomMargin   = 0x0029
	recPrintHeaders   = 0x002A
	recPrintGridlines = 0x002B
	recFont           = 0x0031
	recContinue       = 0x003C
	recWindow1        = 0x003D
	recBackup         = 0x0040
	recCodepage       = 0x0042
	recDefColWidth    = 0x0055
	recWriteAccess    = 0x005C
	recColInfo        = 0x007D
	recGuts           = 0x0080
	recWSBool         = 0x0081
	recGridSet        = 0x0082
	recHCenter        = 0x0083
	recVCenter        = 0x0084
	recBoundSheet     = 0x0085
	recCountry        = 0x008C
	recHideObj        = 0x008D
	recPalette        = 0x0092
	recFnGroupCount   = 0x009C
	recSetup          = 0x00A1
	recMMS            = 0x00C1
	recBookBool       = 0x00DA
	recXF             = 0x00E0
	recInterfaceHdr   = 0x00E1
	recInterfaceEnd   = 0x00E2
	recMergeCells     = 0x00E5
	recSST            = 0x00FC
	recLabelSST       = 0x00FD
	recExtSST         = 0x00FF
	recUseSelfs       = 0x0160
	recDSF            = 0x0161
	recProt4Rev       = 0x01AF
	recRefreshAll     = 0x01B7
	recProt4RevPass   = 0x01BC
	recDimensions     = 0x0200
	recBlank          = 0x0201
	recNumber         = 0x0203
	recRow            = 0x0208
	recIndex          = 0x020B
	recDefRowHeight   = 0x0225
	recWindow2        = 0x023E
	recSaveRecalc     = 0x005F
	recStyle          = 0x0293
	recBOF            = 0x0809
	recTabID          = 0x013D
)

// BOF 子流类型
const (
	bofWorkbookGlobals = 0x0005
	bofWorksheet       = 0x0010
)

// 边框线型
const (
	BorderNone   = 0
	BorderThin   = 1
	BorderMedium = 2
)

// 对齐方式
const (
	HorGeneral = 0
	HorLeft    = 1
	HorCenter  = 2
	VertCenter = 1
)

// ---------------------------------------------------------------------------
// 记录缓冲
// ---------------------------------------------------------------------------

type recBuf struct {
	b []byte
}

func (r *recBuf) rec(id uint16, data []byte) {
	r.b = append(r.b, byte(id), byte(id>>8), byte(len(data)), byte(len(data)>>8))
	r.b = append(r.b, data...)
}

func (r *recBuf) recU16(id uint16, vals ...uint16) {
	d := make([]byte, len(vals)*2)
	for i, v := range vals {
		binary.LittleEndian.PutUint16(d[i*2:], v)
	}
	r.rec(id, d)
}

func appendU16(b []byte, v uint16) []byte {
	var d [2]byte
	binary.LittleEndian.PutUint16(d[:], v)
	return append(b, d[:]...)
}

func appendU32(b []byte, v uint32) []byte {
	var d [4]byte
	binary.LittleEndian.PutUint32(d[:], v)
	return append(b, d[:]...)
}

func f64(v float64) []byte {
	var d [8]byte
	binary.LittleEndian.PutUint64(d[:], math.Float64bits(v))
	return d[:]
}

// ---------------------------------------------------------------------------
// 字体 / XF
// ---------------------------------------------------------------------------

// Font 描述一个 FONT 记录。
type Font struct {
	Name   string // 字体名
	Height int    // 字号，单位 twips（180 = 9 磅）
	Weight int    // 400 常规 / 700 加粗
}

func (r *recBuf) font(f Font) {
	name := encodeUTF16(f.Name)
	d := make([]byte, 0, 16+len(name))
	d = appendU16(d, uint16(f.Height))
	d = appendU16(d, 0)      // 选项标志（字重由 weight 字段决定）
	d = appendU16(d, 0x7FFF) // 颜色：自动
	d = appendU16(d, uint16(f.Weight))
	d = appendU16(d, 0) // 上下标
	d = append(d, 0)    // 下划线
	d = append(d, 0)    // 字体系列
	d = append(d, 0x86) // 字符集：中文 GBK
	d = append(d, 0)    // 保留
	d = append(d, byte(len([]rune(f.Name))))
	d = append(d, 1) // 未压缩 UTF-16LE
	d = append(d, name...)
	r.rec(recFont, d)
}

// 边框颜色位：沿用真实 .xls 文件的取值（左/右为 0x40 自动色）。
const (
	borderColorBits = 0x20400000
	borders2Bits    = 0x04002040 // 上边框自动色 + 填充图案 = 1（实心）
	patternBits     = 0x2089     // 图案前景色 9（白），背景色 0x41
)

// StyleSpec 描述一个单元格 XF。
type StyleSpec struct {
	FontIdx   int
	HorAlign  int
	VertAlign int
	Wrap      bool
	Left      int
	Right     int
	Top       int
	Bottom    int
}

// borderWord 组装 brd1 的低 16 位：left(4) | right(4)<<4 | top(4)<<8 | bottom(4)<<12。
func borderWord(l, rt, t, b int) uint32 {
	return uint32(l&0x0F) | uint32(rt&0x0F)<<4 | uint32(t&0x0F)<<8 | uint32(b&0x0F)<<12
}

// alignByte 组装对齐字节：bit0-2 水平，bit3 自动换行，bit4-6 垂直。
func alignByte(hor, vert int, wrap bool) byte {
	v := hor & 0x07
	if wrap {
		v |= 0x08
	}
	v |= (vert & 0x07) << 4
	return byte(v)
}

func (r *recBuf) xf(s StyleSpec, style bool) {
	d := make([]byte, 20)
	binary.LittleEndian.PutUint16(d[0:], uint16(s.FontIdx))
	binary.LittleEndian.PutUint16(d[2:], 0) // 通用数字格式
	if style {
		binary.LittleEndian.PutUint16(d[4:], 0xFFF5)
		d[9] = 0xF4
	} else {
		binary.LittleEndian.PutUint16(d[4:], 0x0001)
		d[9] = 0xF8
	}
	d[6] = alignByte(s.HorAlign, s.VertAlign, s.Wrap)
	binary.LittleEndian.PutUint32(d[10:], borderWord(s.Left, s.Right, s.Top, s.Bottom)|borderColorBits)
	binary.LittleEndian.PutUint32(d[14:], borders2Bits)
	binary.LittleEndian.PutUint16(d[18:], patternBits)
	r.rec(recXF, d)
}

// ---------------------------------------------------------------------------
// 共享字符串表（SST）
// ---------------------------------------------------------------------------

// SST 是 BIFF8 共享字符串表。重复字符串只存一份，索引被 LABELSST 引用。
type SST struct {
	index map[string]int
	list  []string
	tally []int
}

// NewSST 创建空的共享字符串表。
func NewSST() *SST { return &SST{index: map[string]int{}} }

// Add 返回字符串索引。
func (s *SST) Add(v string) int {
	if i, ok := s.index[v]; ok {
		s.tally[i]++
		return i
	}
	i := len(s.list)
	s.index[v] = i
	s.list = append(s.list, v)
	s.tally = append(s.tally, 1)
	return i
}

// Count 返回字符串总引用次数。
func (s *SST) Count() int {
	n := 0
	for _, c := range s.tally {
		n += c
	}
	return n
}

// Unique 返回去重后的字符串个数。
func (s *SST) Unique() int { return len(s.list) }

// EncodeString 按 BIFF8 规则编码字符串：2 字节字符数 + 1 字节选项 + 字符数组。
// 全部字符 <= 0xFF 时使用压缩（单字节）编码。
func EncodeString(v string) []byte {
	compressed := true
	for _, r := range v {
		if r > 0xFF {
			compressed = false
			break
		}
	}
	units := utf16.Encode([]rune(v))
	out := make([]byte, 0, len(units)*2+3)
	out = appendU16(out, uint16(len(units)))
	if compressed {
		out = append(out, 0)
		for _, r := range v {
			out = append(out, byte(r))
		}
		return out
	}
	out = append(out, 1)
	for _, c := range units {
		out = appendU16(out, c)
	}
	return out
}

// Build 生成 SST 记录与必要的 CONTINUE 记录。
func (s *SST) Build() []byte {
	const maxPiece = 0x2020 // BIFF8 单记录数据上限

	out := &recBuf{}
	cur := make([]byte, 8) // 前 8 字节：总引用数 + 唯一字符串数
	pieces := [][]byte{}
	flush := func() {
		pieces = append(pieces, cur)
		cur = make([]byte, 0, maxPiece)
	}

	for i, v := range s.list {
		if s.tally[i] == 0 {
			v = ""
		}
		raw := EncodeString(v)
		compressed := raw[2] == 0
		// 原子部分：长度字段 + 选项字段 + 首个字符，必须落在同一记录内。
		atom := 4
		if !compressed {
			atom = 5
		}
		if len(raw) < atom {
			atom = len(raw)
		}
		if maxPiece-len(cur) < atom {
			flush()
		}
		cur = append(cur, raw[:atom]...)
		rest := raw[atom:]
		unit := 1
		if !compressed {
			unit = 2
		}
		for len(rest) > 0 {
			free := maxPiece - len(cur)
			if free >= len(rest) {
				cur = append(cur, rest...)
				break
			}
			n := free
			if unit == 2 {
				n &^= 1
			}
			cur = append(cur, rest[:n]...)
			rest = rest[n:]
			flush()
			// CONTINUE 开头重复一次选项标志（仅保留压缩位）。
			if compressed {
				cur = append(cur, 0)
			} else {
				cur = append(cur, 1)
			}
		}
	}
	flush()

	binary.LittleEndian.PutUint32(pieces[0][0:], uint32(s.Count()))
	binary.LittleEndian.PutUint32(pieces[0][4:], uint32(len(s.list)))
	out.rec(recSST, pieces[0])
	for _, p := range pieces[1:] {
		out.rec(recContinue, p)
	}
	return out.b
}

// ---------------------------------------------------------------------------
// 工作表模型
// ---------------------------------------------------------------------------

type cellKind uint8

const (
	cellBlank cellKind = iota
	cellText
	cellNumber
)

// Cell 是一个工作表单元格。
type Cell struct {
	kind  cellKind
	text  string
	num   float64
	style int
}

// Row 是一行。
type Row struct {
	Height int // 行高（twips）
	first  int
	last   int
	cells  map[int]Cell
}

// Value 返回单元格内容的文本表示；空白单元格返回空串。
func (c Cell) Value() string {
	switch c.kind {
	case cellText:
		return c.text
	case cellNumber:
		return strconv.FormatFloat(c.num, 'f', -1, 64)
	}
	return ""
}

// Style 返回单元格引用的 XF 索引。
func (c Cell) Style() int { return c.style }

// IsNumber 报告单元格是否为数值单元格。
func (c Cell) IsNumber() bool { return c.kind == cellNumber }

// Merge 是一个合并区域（行/列均为闭区间）。
type Merge struct{ RowFirst, RowLast, ColFirst, ColLast int }

// Sheet 是一张工作表。
type Sheet struct {
	Name       string
	ColWidths  []int // 单位 1/256 个字符宽
	Rows       []Row
	Merges     []Merge
	StyleIndex int // COLINFO/ROW 记录的默认 XF 索引
}

func (s *Sheet) row(r int) *Row {
	for len(s.Rows) <= r {
		s.Rows = append(s.Rows, Row{Height: 285, cells: map[int]Cell{}})
	}
	if s.Rows[r].cells == nil {
		s.Rows[r].cells = map[int]Cell{}
	}
	return &s.Rows[r]
}

func (s *Sheet) put(r, c int, cell Cell) {
	row := s.row(r)
	row.cells[c] = cell
	if c+1 > row.last {
		row.last = c + 1
	}
	if c < row.first {
		row.first = c
	}
}

// SetText 写入文本单元格；空字符串写成带样式的空单元格，以保留边框。
func (s *Sheet) SetText(r, c int, v string, style int) {
	if v == "" {
		s.put(r, c, Cell{kind: cellBlank, style: style})
		return
	}
	s.put(r, c, Cell{kind: cellText, text: v, style: style})
}

// SetNumber 写入数值单元格。
func (s *Sheet) SetNumber(r, c int, v float64, style int) {
	s.put(r, c, Cell{kind: cellNumber, num: v, style: style})
}

// SetHeight 设置行高。
func (s *Sheet) SetHeight(r, h int) { s.row(r).Height = h }

// CellAt 返回指定位置的单元格。
func (s *Sheet) CellAt(r, c int) (Cell, bool) {
	if r < 0 || r >= len(s.Rows) || s.Rows[r].cells == nil {
		return Cell{}, false
	}
	cell, ok := s.Rows[r].cells[c]
	return cell, ok
}

// Height 返回行高（twips）；行不存在时返回 0。
func (s *Sheet) Height(r int) int {
	if r < 0 || r >= len(s.Rows) {
		return 0
	}
	return s.Rows[r].Height
}

// SetStyle 覆盖已有单元格的样式；单元格尚不存在时补一个空单元格，
// 使框线得以保留（用于给全表最后一行换用收口样式）。
func (s *Sheet) SetStyle(r, c, style int) {
	row := s.row(r)
	if cell, ok := row.cells[c]; ok {
		cell.style = style
		row.cells[c] = cell
		return
	}
	s.put(r, c, Cell{kind: cellBlank, style: style})
}

// AddMerge 追加合并区域。
func (s *Sheet) AddMerge(r1, r2, c1, c2 int) {
	s.Merges = append(s.Merges, Merge{r1, r2, c1, c2})
}

// ---------------------------------------------------------------------------
// 工作簿
// ---------------------------------------------------------------------------

// 单元格 XF 索引（16 个样式 XF 之后）。
// 单元格样式索引（16 个「样式 XF」之后）。每一项都与真实文件「新.xls」
// 逐条对应；注释中的四元组为框线线型 (左, 右, 上, 下)，0 无 / 1 细 / 2 中粗。
const (
	XfHeaderLeft   = 16 // 表头「序号」「图元位置」：(1,1,2,1)
	XfHeaderRight  = 17 // 表头「工程量计算式」：(1,2,2,1)
	XfGroupLeft    = 18 // 构件标题行前两列：(1,1,1,1)
	XfGroupRight   = 19 // 构件标题行末列：(1,2,1,1)
	XfSerial       = 20 // 序号：居中
	XfPosition     = 21 // 图元位置
	XfNameRight    = 22 // 计算式首行（「柱: KZ9」）：无下框线 (1,2,1,0)
	XfDetailRight  = 23 // 计算式明细行：无上框线 (1,2,0,1)
	XfLastSerial   = 24 // 全表末行 序号：下框线中粗 (1,1,1,2)
	XfLastPosition = 25 // 全表末行 图元位置：下框线中粗 (1,1,1,2)
	XfLastDetail   = 26 // 全表末行 计算式明细：无上框线 + 下框线中粗 (1,2,0,2)

	// 下面三个是通用表格样式，供比对结果等「非计算书版式」使用：
	// 四边统一细框线，不区分首末列。
	XfTableHead   = 27 // 表头：加粗、居中
	XfTableCell   = 28 // 正文：常规、左对齐、自动换行
	XfTableCenter = 29 // 正文：常规、居中
)

// cellFontBold 报告字体索引是否加粗（真实文件中 7、8、13、14、15 为加粗：
// 分别是表头、构件标题行的前后两列，以及计算式首行的构件名）。
func cellFontBold(i int) bool {
	switch i {
	case 7, 8, 13, 14, 15:
		return true
	}
	return false
}

// Workbook 是待写出的工作簿。
type Workbook struct {
	Sheet      *Sheet
	NormalFont int
	BoldFont   int
	sst        *SST
}

// NewWorkbook 创建带默认字体与样式的空工作簿。
func NewWorkbook(sheet *Sheet) *Workbook {
	sheet.StyleIndex = 15
	return &Workbook{Sheet: sheet, NormalFont: 5, BoldFont: 6, sst: NewSST()}
}

// Write 生成完整的 .xls 文件字节（OLE2 容器内的 Workbook 流）。
func (wb *Workbook) Write() ([]byte, error) {
	sheetData := wb.sheetStream() // 会填充共享字符串表
	globals := wb.globals(0)
	// BOUNDSHEET 中记录工作表 BOF 的绝对偏移，长度与占位值无关，故二次生成。
	globals = wb.globals(len(globals))
	return append(globals, sheetData...), nil
}

func (wb *Workbook) globals(sheetPos int) []byte {
	r := &recBuf{}
	bof := make([]byte, 16)
	binary.LittleEndian.PutUint16(bof[0:], 0x0600)
	binary.LittleEndian.PutUint16(bof[2:], bofWorkbookGlobals)
	binary.LittleEndian.PutUint16(bof[4:], 0x0DBB)
	binary.LittleEndian.PutUint16(bof[6:], 0x07CC)
	binary.LittleEndian.PutUint32(bof[8:], 0x00000041)
	binary.LittleEndian.PutUint32(bof[12:], 0x00000006)
	r.rec(recBOF, bof)

	r.recU16(recInterfaceHdr, 0x04B0)
	r.recU16(recMMS, 0)
	r.rec(recInterfaceEnd, nil)
	// WRITEACCESS：BIFF8 字符串（2 字节字符数 + 1 字节选项 + 字符数），
	// 整条记录固定 112 字节，多余部分以空格 0x20 补齐（与 Excel 写出的形态一致）。
	user := "zhiduoxing2xls"
	wa := make([]byte, 112)
	for i := range wa {
		wa[i] = 0x20
	}
	binary.LittleEndian.PutUint16(wa[0:], uint16(len([]rune(user))))
	wa[2] = 0x01 // 选项：0x01 = UTF-16LE 未压缩
	copy(wa[3:], encodeUTF16(user))
	r.rec(recWriteAccess, wa)
	r.recU16(recCodepage, 0x04B0)
	r.recU16(recDSF, 0)
	r.recU16(recTabID, 1)
	r.recU16(recFnGroupCount, 14)
	r.recU16(recPrecision, 1)
	r.recU16(recWindowProtect, 0)
	r.recU16(recProtect, 0)
	r.recU16(recPassword, 0)
	r.recU16(recProt4Rev, 0)
	r.recU16(recProt4RevPass, 0)
	r.recU16(recWindow1, 0x01E0, 0x005A, 0x3FCF, 0x2A4E, 0x0038, 0, 0, 1, 0x0258)
	r.recU16(recBackup, 0)
	r.recU16(recHideObj, 0)
	r.recU16(recDate1904, 0)
	r.recU16(recRefreshAll, 0)
	r.recU16(recBookBool, 0)

	// 字体表。注意 BIFF8 的字体索引 4 是保留占位项，真实文件并不写出该条
	// FONT 记录，读取器会自动补位 —— 所以这里只按顺序写出 17 条：
	// 0-3 保留占位、[4] 默认常规字体（索引 5）、[5] 默认加粗字体（索引 6），
	// 随后的 11 条依次对应索引 7-17，供各单元格样式引用（7、8、13、14 加粗）。
	// 前 6 条（占位 + Normal 样式字体）与真实文件一样是 Arial 10 磅，
	// 各单元格样式实际引用的则统一为宋体 9 磅。
	regular := Font{Name: "宋体", Height: 180, Weight: 400}
	bold := Font{Name: "宋体", Height: 180, Weight: 700}
	baseRegular := Font{Name: "Arial", Height: 200, Weight: 400}
	baseBold := Font{Name: "Arial", Height: 200, Weight: 700}
	for i := 0; i < 4; i++ {
		r.font(baseRegular)
	}
	r.font(baseRegular)
	r.font(baseBold)
	for i := 7; i <= 17; i++ {
		if cellFontBold(i) {
			r.font(bold)
		} else {
			r.font(regular)
		}
	}

	// 16 个样式 XF（索引 0-15），供行/列默认格式与样式记录引用。
	for i := 0; i < 16; i++ {
		r.xf(StyleSpec{FontIdx: wb.NormalFont, HorAlign: HorGeneral, VertAlign: VertCenter}, true)
	}
	// 单元格 XF（索引 16-26）：字段顺序为 字体/水平/垂直/自动换行/左/右/上/下。
	// 字体索引与框线线型均按真实文件还原，最后一行用中粗下框线收口。
	cellStyles := []StyleSpec{
		{7, HorCenter, VertCenter, true, BorderThin, BorderThin, BorderMedium, BorderThin},     // 16 表头-序号/位置
		{13, HorCenter, VertCenter, true, BorderThin, BorderMedium, BorderMedium, BorderThin},  // 17 表头-计算式
		{8, HorGeneral, VertCenter, true, BorderThin, BorderThin, BorderThin, BorderThin},      // 18 构件标题-前两列
		{14, HorGeneral, VertCenter, true, BorderThin, BorderMedium, BorderThin, BorderThin},   // 19 构件标题-末列
		{9, HorCenter, VertCenter, true, BorderThin, BorderThin, BorderThin, BorderThin},       // 20 序号
		{11, HorGeneral, VertCenter, true, BorderThin, BorderThin, BorderThin, BorderThin},     // 21 图元位置
		{15, HorGeneral, VertCenter, true, BorderThin, BorderMedium, BorderThin, BorderNone},   // 22 计算式首行
		{16, HorGeneral, VertCenter, true, BorderThin, BorderMedium, BorderNone, BorderThin},   // 23 计算式明细行
		{10, HorCenter, VertCenter, true, BorderThin, BorderThin, BorderThin, BorderMedium},    // 24 末行-序号
		{12, HorGeneral, VertCenter, true, BorderThin, BorderThin, BorderThin, BorderMedium},   // 25 末行-图元位置
		{17, HorGeneral, VertCenter, true, BorderThin, BorderMedium, BorderNone, BorderMedium}, // 26 末行-计算式明细
		{13, HorCenter, VertCenter, true, BorderThin, BorderThin, BorderThin, BorderThin},      // 27 通用表头
		{9, HorGeneral, VertCenter, true, BorderThin, BorderThin, BorderThin, BorderThin},      // 28 通用正文
		{9, HorCenter, VertCenter, true, BorderThin, BorderThin, BorderThin, BorderThin},       // 29 通用居中
	}
	for _, st := range cellStyles {
		r.xf(st, false)
	}
	// 内置样式记录（常规 / 百分比 / 货币等），沿用真实文件的写法。
	for _, v := range []uint16{0x8010, 0x8011, 0x8000, 0x8012, 0x8013, 0x8014} {
		r.rec(recStyle, []byte{byte(v), byte(v >> 8), 0x00, 0xFF})
	}
	r.recU16(recUseSelfs, 0)

	// BOUNDSHEET：工作表名与 BOF 绝对偏移。
	name := []rune(wb.Sheet.Name)
	if len(name) > 31 {
		name = name[:31]
	}
	bs := make([]byte, 0, 8+len(name)*2)
	bs = appendU32(bs, uint32(sheetPos))
	bs = append(bs, 0x00, 0x00)
	bs = append(bs, byte(len(name)), 1)
	bs = append(bs, encodeUTF16(string(name))...)
	r.rec(recBoundSheet, bs)

	r.recU16(recCountry, 1, 1)

	// SST：位置需在写入前确定，以便构建 EXTSST。
	sstPos := len(r.b)
	r.b = append(r.b, wb.sst.Build()...)
	if wb.sst.Unique() > 0 {
		ex := make([]byte, 0, 12)
		ex = appendU16(ex, 8)
		ex = appendU32(ex, uint32(sstPos+4))
		ex = appendU16(ex, 0)
		ex = appendU16(ex, 0)
		r.rec(recExtSST, ex)
	}
	r.rec(recEOF, nil)
	return r.b
}

func (wb *Workbook) sheetStream() []byte {
	s := wb.Sheet
	r := &recBuf{}
	bof := make([]byte, 16)
	binary.LittleEndian.PutUint16(bof[0:], 0x0600)
	binary.LittleEndian.PutUint16(bof[2:], bofWorksheet)
	binary.LittleEndian.PutUint16(bof[4:], 0x0DBB)
	binary.LittleEndian.PutUint16(bof[6:], 0x07CC)
	binary.LittleEndian.PutUint32(bof[8:], 0x00000041)
	binary.LittleEndian.PutUint32(bof[12:], 0x00000006)
	r.rec(recBOF, bof)

	r.recU16(recCalcMode, 1)
	r.recU16(recCalcCount, 100)
	r.recU16(recSaveRecalc, 0)
	r.recU16(recDelta, 0)
	r.recU16(recIteration, 0)
	r.rec(recGuts, make([]byte, 8))
	r.recU16(recDefRowHeight, 0, 0x00FF)
	r.recU16(recWSBool, 0x04C1)
	r.recU16(recDefColWidth, 8) // 默认列宽 8 个字符，与真实文件一致

	for i, w := range s.ColWidths {
		r.recU16(recColInfo, uint16(i), uint16(i), uint16(w), uint16(s.StyleIndex), 0, 0)
	}

	dim := make([]byte, 14)
	binary.LittleEndian.PutUint32(dim[4:], uint32(maxInt(len(s.Rows)-1, 0)))
	binary.LittleEndian.PutUint16(dim[10:], uint16(maxInt(len(s.ColWidths)-1, 0)))
	r.rec(recDimensions, dim)

	r.recU16(recPrintHeaders, 0)
	r.recU16(recPrintGridlines, 0)
	r.recU16(recGridSet, 1)
	r.rec(recHeader, []byte{0, 0})
	r.rec(recFooter, []byte{0, 0})
	r.recU16(recHCenter, 0)
	r.recU16(recVCenter, 0)
	r.rec(recLeftMargin, f64(0.7))
	r.rec(recRightMargin, f64(0.7))
	r.rec(recTopMargin, f64(0.75))
	r.rec(recBottomMargin, f64(0.75))
	setup := make([]byte, 34)
	binary.LittleEndian.PutUint16(setup[0:], 9)   // A4
	binary.LittleEndian.PutUint16(setup[2:], 100) // 缩放 100%
	binary.LittleEndian.PutUint16(setup[4:], 1)   // 起始页码
	r.rec(recSetup, setup)
	r.recU16(recProtect, 0)
	r.recU16(recScenProtect, 0)
	r.recU16(recWindowProtect, 0)
	r.recU16(recObjectProtect, 0)
	r.recU16(recPassword, 0)

	// ROW 记录
	for i := range s.Rows {
		row := &s.Rows[i]
		grbit := uint32(0x0100)
		if row.Height != 0x00FF {
			grbit |= 0x0040 // 行高与默认字体高度不一致
		}
		d := make([]byte, 16)
		binary.LittleEndian.PutUint16(d[0:], uint16(i))
		binary.LittleEndian.PutUint16(d[2:], uint16(row.first))
		binary.LittleEndian.PutUint16(d[4:], uint16(row.last))
		binary.LittleEndian.PutUint16(d[6:], uint16(row.Height))
		binary.LittleEndian.PutUint32(d[12:], grbit|uint32(s.StyleIndex)<<16)
		r.rec(recRow, d)
	}

	// 单元格记录：按行、列升序
	for i := range s.Rows {
		row := &s.Rows[i]
		cols := make([]int, 0, len(row.cells))
		for c := range row.cells {
			cols = append(cols, c)
		}
		sort.Ints(cols)
		for _, c := range cols {
			cell := row.cells[c]
			switch cell.kind {
			case cellNumber:
				d := make([]byte, 14)
				binary.LittleEndian.PutUint16(d[0:], uint16(i))
				binary.LittleEndian.PutUint16(d[2:], uint16(c))
				binary.LittleEndian.PutUint16(d[4:], uint16(cell.style))
				binary.LittleEndian.PutUint64(d[6:], math.Float64bits(cell.num))
				r.rec(recNumber, d)
			case cellText:
				idx := wb.sst.Add(cell.text)
				d := make([]byte, 10)
				binary.LittleEndian.PutUint16(d[0:], uint16(i))
				binary.LittleEndian.PutUint16(d[2:], uint16(c))
				binary.LittleEndian.PutUint16(d[4:], uint16(cell.style))
				binary.LittleEndian.PutUint32(d[6:], uint32(idx))
				r.rec(recLabelSST, d)
			default:
				d := make([]byte, 6)
				binary.LittleEndian.PutUint16(d[0:], uint16(i))
				binary.LittleEndian.PutUint16(d[2:], uint16(c))
				binary.LittleEndian.PutUint16(d[4:], uint16(cell.style))
				r.rec(recBlank, d)
			}
		}
	}

	if len(s.Merges) > 0 {
		d := make([]byte, 0, 2+len(s.Merges)*8)
		d = appendU16(d, uint16(len(s.Merges)))
		for _, m := range s.Merges {
			d = appendU16(d, uint16(m.RowFirst))
			d = appendU16(d, uint16(m.RowLast))
			d = appendU16(d, uint16(m.ColFirst))
			d = appendU16(d, uint16(m.ColLast))
		}
		r.rec(recMergeCells, d)
	}

	r.recU16(recWindow2, 0x06B6, 0, 0x4000, 0, 0, 0, 0, 0, 0)
	r.rec(recEOF, nil)
	return r.b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
