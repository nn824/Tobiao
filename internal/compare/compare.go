// Package compare 把两份「绘图输入构件工程量计算书」按坐标逐条比对。
//
// 比对键是「楼层 + 构件名 + 图元位置（坐标）」：同一坐标、同一构件下，
// 把两侧的「工程量计算式」各自收成一个集合，再比较两个集合是否一致。
// 这样即使两侧的图元排列顺序不同，只要同一坐标上的算式相同就算一致。
package compare

import (
	"bytes"
	"sort"
	"strings"

	"github.com/ninglang/zhiduoxing-xls/internal/render"
	"github.com/ninglang/zhiduoxing-xls/internal/xls"
)

// 比对结果。
const (
	StatusSame    = "一致"
	StatusDiff    = "不一致"
	StatusOnlyGen = "仅计算书有"
	StatusOnlyRef = "仅参考文件有"
)

// Row 是计算书里的一条图元明细。
type Row struct {
	Floor    string // 楼层名
	TypeName string // 构件类型，如「柱」
	Name     string // 构件名，如 KZ9
	Position string // 图元位置（坐标），如 <6,D>
	Formula  string // 工程量计算式（多行）
}

// Key 是比对用的唯一键：楼层 + 构件名 + 坐标。
type Key struct {
	Floor    string
	Name     string
	Position string
}

// Item 是汇总后的一行比对结果。
type Item struct {
	Key
	TypeName  string
	Generated []string // 计算书里该键下的全部计算式
	Reference []string // 参考文件里该键下的全部计算式
	Status    string
}

// Parse 从工作表解析出图元明细。
//
// floor 非空时用它覆盖行内的楼层名（需求是「楼层名按测试结果 JSON 输出」，
// 由调用方把 JSON 推导出的楼层名传进来）。
func Parse(s *xls.Sheet, floor string) []Row {
	if s == nil {
		return nil
	}
	var out []Row
	curFloor, curType := "", ""
	for r := 0; r < len(s.Rows); r++ {
		c0, ok0 := s.CellAt(r, 0)
		c1, ok1 := s.CellAt(r, 1)
		c2, _ := s.CellAt(r, 2)

		// 构件分组标题行：「首层 - 柱 - KZ9」，三列写着同一段文本。
		if isTitleRow(c0, c1, c2) {
			f, t, _ := splitTitle(c2.Value())
			if f != "" {
				curFloor = f
			}
			if t != "" {
				curType = t
			}
			continue
		}

		// 图元行：序号列是数字。第 r 行是「构件名」，第 r+1 行是「计算式」。
		if !ok0 || !c0.IsNumber() || !ok1 {
			continue
		}
		pos := strings.TrimSpace(c1.Value())
		typ, name := splitName(strings.TrimSpace(c2.Value()))
		if typ == "" {
			typ = curType
		}

		formula := ""
		if r+1 < len(s.Rows) {
			if n0, ok := s.CellAt(r+1, 0); ok && n0.IsNumber() && n0.Value() == c0.Value() {
				if d, ok := s.CellAt(r+1, 2); ok {
					formula = d.Value()
				}
				r++ // 明细行已消费
			}
		}

		f := floor
		if f == "" {
			f = curFloor
		}
		out = append(out, Row{Floor: f, TypeName: typ, Name: name, Position: pos, Formula: formula})
	}
	return out
}

// isTitleRow 判断一行是不是构件分组标题（三列文本相同且含 " - "）。
func isTitleRow(c0, c1, c2 xls.Cell) bool {
	a, b, c := c0.Value(), c1.Value(), c2.Value()
	if a == "" || a != b || b != c {
		return false
	}
	return strings.Contains(a, " - ")
}

// splitTitle 把「首层 - 柱 - KZ9」拆成楼层、类型、构件名。
// 楼层名本身不含空格，构件名可能带 " - "，所以从两端各切一次。
func splitTitle(s string) (floor, typ, name string) {
	i := strings.Index(s, " - ")
	if i < 0 {
		return "", "", strings.TrimSpace(s)
	}
	floor = strings.TrimSpace(s[:i])
	rest := s[i+3:]
	if j := strings.LastIndex(rest, " - "); j >= 0 {
		return floor, strings.TrimSpace(rest[:j]), strings.TrimSpace(rest[j+3:])
	}
	return floor, strings.TrimSpace(rest), ""
}

// splitName 把「柱: KZ9」拆成类型与构件名。
func splitName(s string) (typ, name string) {
	for _, sep := range []string{": ", "：", ":"} {
		if i := strings.Index(s, sep); i > 0 {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(sep):])
		}
	}
	return "", s
}

// Collect 按「楼层 + 构件名 + 坐标」归并两侧明细，返回比对结果。
// 顺序优先按计算书中的出现顺序，参考文件独有的条目追加在末尾。
func Collect(gen, ref []Row) []Item {
	genMap := make(map[Key][]string, len(gen))
	refMap := make(map[Key][]string, len(ref))
	types := make(map[Key]string)

	var order []Key
	seen := make(map[Key]bool, len(gen)+len(ref))
	addKey := func(k Key) {
		if !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}

	for _, r := range gen {
		k := Key{r.Floor, r.Name, r.Position}
		addKey(k)
		genMap[k] = append(genMap[k], r.Formula)
		if _, ok := types[k]; !ok && r.TypeName != "" {
			types[k] = r.TypeName
		}
	}
	for _, r := range ref {
		k := Key{r.Floor, r.Name, r.Position}
		addKey(k)
		refMap[k] = append(refMap[k], r.Formula)
		if _, ok := types[k]; !ok && r.TypeName != "" {
			types[k] = r.TypeName
		}
	}

	items := make([]Item, 0, len(order))
	for _, k := range order {
		g, rf := genMap[k], refMap[k]
		status := StatusDiff
		switch {
		case len(g) > 0 && len(rf) > 0:
			if normalize(g) == normalize(rf) {
				status = StatusSame
			}
		case len(g) > 0:
			status = StatusOnlyGen
		default:
			status = StatusOnlyRef
		}
		items = append(items, Item{Key: k, TypeName: types[k], Generated: g, Reference: rf, Status: status})
	}
	return items
}

// normalize 把一组计算式压成可比较的字符串：
// 统一换行、去掉每行首尾空白与空行，再排序 —— 顺序不同不算差异。
func normalize(list []string) string {
	lines := make([]string, 0, len(list))
	for _, s := range list {
		for _, ln := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
			if ln = strings.TrimSpace(ln); ln != "" {
				lines = append(lines, ln)
			}
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// 输出 .xls
// ---------------------------------------------------------------------------

// 比对表的列宽（单位 1/256 字符）。
var colWidths = []int{1500, 2600, 3200, 3600, 5400, 12000, 12000, 3400}

// cellCapacity 是计算式列一行能放的显示宽度（中文字符按 2 计）。
const cellCapacity = 46

// WriteOptions 控制比对报告的外观。
type WriteOptions struct {
	SheetName string // 工作表名，留空取「工程量计算式比对」
}

// Build 生成比对报告的 .xls 字节流（OLE2 容器已封装）。
func Build(items []Item, opt WriteOptions) ([]byte, error) {
	name := strings.TrimSpace(opt.SheetName)
	if name == "" {
		name = "工程量计算式比对"
	}
	sheet := &xls.Sheet{Name: name, ColWidths: colWidths}
	wb := xls.NewWorkbook(sheet)

	head := []string{"序号", "楼层", "构件类型", "构件名称", "图元位置",
		"工程量计算式（计算书）", "工程量计算式（参考文件）", "比对结果"}
	for c, h := range head {
		sheet.SetText(0, c, h, xls.XfTableHead)
	}
	sheet.SetHeight(0, render.RowHeightBase)

	for i, it := range items {
		r := i + 1
		gen := strings.Join(it.Generated, "\n")
		ref := strings.Join(it.Reference, "\n")

		sheet.SetNumber(r, 0, float64(i+1), xls.XfTableCenter)
		sheet.SetText(r, 1, it.Floor, xls.XfTableCenter)
		sheet.SetText(r, 2, it.TypeName, xls.XfTableCenter)
		sheet.SetText(r, 3, it.Name, xls.XfTableCell)
		sheet.SetText(r, 4, it.Position, xls.XfTableCell)
		sheet.SetText(r, 5, gen, xls.XfTableCell)
		sheet.SetText(r, 6, ref, xls.XfTableCell)
		sheet.SetText(r, 7, it.Status, xls.XfTableCenter)

		lines := render.DisplayLines(gen, cellCapacity)
		if n := render.DisplayLines(ref, cellCapacity); n > lines {
			lines = n
		}
		sheet.SetHeight(r, render.RowHeight(lines))
	}

	stream, err := wb.Write()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Grow(len(stream) + 512)
	if err := xls.WriteCFB(&buf, "Workbook", stream); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Stats 汇总比对结果的计数。
type Stats struct {
	Items         int
	Same          int
	Diff          int
	OnlyGenerated int
	OnlyReference int
}

// Count 统计各状态的条目数。
func Count(items []Item) Stats {
	st := Stats{Items: len(items)}
	for _, it := range items {
		switch it.Status {
		case StatusSame:
			st.Same++
		case StatusDiff:
			st.Diff++
		case StatusOnlyGen:
			st.OnlyGenerated++
		case StatusOnlyRef:
			st.OnlyReference++
		}
	}
	return st
}
