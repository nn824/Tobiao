// Package build 把智多星计算结果 JSON 转换成「绘图输入构件工程量计算书」表格。
package build

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/nn824/Tobiao/internal/model"
	"github.com/nn824/Tobiao/internal/render"
	"github.com/nn824/Tobiao/internal/spec"
	"github.com/nn824/Tobiao/internal/xls"
)

// 列宽（单位 1/256 字符宽），与真实文件一致：
// 实际只用前 3 列，第 4 列与第 3 列同宽，是 Excel 记录列宽的收尾项。
var colWidths = []int{1865, 5522, 17518, 17518}

// Options 是转换参数。
type Options struct {
	Section      string            // 计算结果分组：norm（默认）或 list
	FloorName    string            // 楼层名；为空时按 LCH 推导
	SheetName    string            // 工作表名；为空时按「绘图输入构件工程量计算书-楼层」生成
	EmitQuantity bool              // 是否输出「数量」行（JSON 未导出该量，按图元恒为 1 生成）
	AppendExtra  bool              // 是否把类型规则之外的工程量追加到明细末尾
	Positions    map[string]string // CID → 图元位置（同一 CID 只有一条时用它）
	// PositionFor 返回图元位置，非 nil 时优先于 Positions。
	// 入参是构件的 CID（定义 id）与 PID（实例 id）；PID 在每个图元上唯一，
	// 是与三维模型对齐的首选键，CID 用于没有 PID 时按类型顺序兜底。
	PositionFor func(cid, pid string) string
}

// Report 记录一次转换的诊断信息。
type Report struct {
	Section       string
	Floor         string
	SheetName     string
	Components    int
	Groups        int
	Positioned    int            // 成功填上「图元位置」的图元数
	Skipped       map[string]int // 跳过原因 → 图元数
	UnknownZL     map[string]int // 未收录构件子类 → 图元数
	UnmappedCodes map[string]int // 未收录工程量编码 → 出现次数
	HiddenCodes   map[string]int // 属于类型规则之外、未输出的编码 → 出现次数
	SectionErrors []string
	NullRules     int
	Warnings      []string
}

func newReport() *Report {
	return &Report{
		Skipped:       map[string]int{},
		UnknownZL:     map[string]int{},
		UnmappedCodes: map[string]int{},
		HiddenCodes:   map[string]int{},
	}
}

func (r *Report) warnf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	for _, w := range r.Warnings {
		if w == msg {
			return
		}
	}
	if len(r.Warnings) < 60 {
		r.Warnings = append(r.Warnings, msg)
	}
}

// Convert 执行转换并返回工作簿构造结果。
func Convert(doc *model.Document, table *spec.Table, opt Options) (*xls.Workbook, *Report, error) {
	rep := newReport()

	sec := spec.SectionOf(doc, opt.Section)
	rep.Section = spec.SectionName(opt.Section)
	rep.NullRules = len(sec.Null)

	floor := sec.FirstFloor()
	lch := 0
	if floor != nil {
		lch = int(floor.LCH)
	}
	rep.Floor = opt.FloorName
	if rep.Floor == "" {
		rep.Floor = spec.FloorName(lch)
	}
	rep.SheetName = opt.SheetName
	if rep.SheetName == "" {
		rep.SheetName = "绘图输入构件工程量计算书-" + rep.Floor
	}

	// 分组：按构件子类排序，组内保持模型中的出现顺序。
	type group struct {
		zl    string
		mc    string
		comps []model.Component
	}
	groups := []*group{}
	index := map[string]*group{}
	for _, c := range sec.Components() {
		rep.Components++
		if reason := table.Skipped(c.ZL); reason != "" {
			rep.Skipped[reason]++
			continue
		}
		if len(c.Quantities) == 0 {
			rep.Skipped["无工程量"]++
			continue
		}
		key := c.ZL + "\x00" + c.MC
		g, ok := index[key]
		if !ok {
			g = &group{zl: c.ZL, mc: c.MC}
			index[key] = g
			groups = append(groups, g)
		}
		g.comps = append(g.comps, c)
	}
	// 稳定排序：先按类型权重，再按首次出现顺序。
	sort.SliceStable(groups, func(i, j int) bool {
		oi, oj := table.Order(groups[i].zl), table.Order(groups[j].zl)
		if oi != oj {
			return oi < oj
		}
		return false
	})
	rep.Groups = len(groups)

	sheet := &xls.Sheet{Name: rep.SheetName, ColWidths: colWidths}
	wb := xls.NewWorkbook(sheet)

	// 表头
	sheet.SetText(0, 0, "序号", xls.XfHeaderLeft)
	sheet.SetText(0, 1, "图元位置", xls.XfHeaderLeft)
	sheet.SetText(0, 2, "工程量计算式", xls.XfHeaderRight)
	sheet.SetHeight(0, render.RowHeightBase)

	row := 1
	lastDetailRow := -1
	for _, g := range groups {
		typeName := table.TypeName(g.zl)
		ts, known := table.Lookup(g.zl)
		if !known {
			rep.UnknownZL[g.zl] += len(g.comps)
			rep.warnf("构件子类 %s 未收录，按「%s」输出且明细顺序取数据原顺序", g.zl, typeName)
		}

		// 构件分组标题行（跨 3 列合并）。真实文件在合并区的每个格子里都写了
		// 同一段文本，这里同样重复写入，保持与参照文件逐格一致。
		title := fmt.Sprintf("%s - %s - %s", rep.Floor, typeName, g.mc)
		sheet.SetText(row, 0, title, xls.XfGroupLeft)
		sheet.SetText(row, 1, title, xls.XfGroupLeft)
		sheet.SetText(row, 2, title, xls.XfGroupRight)
		sheet.SetHeight(row, render.RowHeightBase)
		sheet.AddMerge(row, row, 0, 2)
		row++

		for i := range g.comps {
			c := &g.comps[i]
			detail := renderDetail(c, ts, known, table, opt, rep)
			pos := lookupPosition(opt, c.CID, c.PID)
			if pos != "" {
				rep.Positioned++
			}

			nameRow := row
			detailRow := row + 1

			// 序号列与图元位置列竖向合并两行，两行都写入相同的值（同参照文件）。
			sheet.SetNumber(nameRow, 0, float64(i+1), xls.XfSerial)
			sheet.SetText(nameRow, 1, pos, xls.XfPosition)
			sheet.SetText(nameRow, 2, typeName+": "+g.mc, xls.XfNameRight)
			sheet.SetNumber(detailRow, 0, float64(i+1), xls.XfSerial)
			sheet.SetText(detailRow, 1, pos, xls.XfPosition)
			sheet.SetText(detailRow, 2, detail, xls.XfDetailRight)

			sheet.SetHeight(nameRow, render.RowHeightBase)
			sheet.SetHeight(detailRow, render.RowHeight(render.DisplayLines(detail, render.CellCapacity)))
			sheet.AddMerge(nameRow, detailRow, 0, 0)
			sheet.AddMerge(nameRow, detailRow, 1, 1)
			lastDetailRow = detailRow
			row += 2
		}
	}

	// 全表最后一行换成中粗下框线，给整张表收口（同参照文件）。
	if lastDetailRow > 0 {
		sheet.SetStyle(lastDetailRow, 0, xls.XfLastSerial)
		sheet.SetStyle(lastDetailRow, 1, xls.XfLastPosition)
		sheet.SetStyle(lastDetailRow, 2, xls.XfLastDetail)
	}

	if len(sec.Error) > 0 {
		keys := make([]string, 0, len(sec.Error))
		for k := range sec.Error {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			first := strings.SplitN(strings.TrimSpace(sec.Error[k]), "\n", 2)[0]
			rep.SectionErrors = append(rep.SectionErrors, k+" : "+first)
		}
	}
	if floor == nil {
		rep.warnf("在 %s 分组中未找到楼层数据", rep.Section)
	}
	return wb, rep, nil
}

// lookupPosition 取图元位置：优先用回调（按 PID 精确对齐），其次用静态对照表。
func lookupPosition(opt Options, cid, pid string) string {
	if opt.PositionFor != nil {
		return opt.PositionFor(cid, pid)
	}
	if opt.Positions != nil {
		return opt.Positions[cid]
	}
	return ""
}

// renderDetail 渲染一个图元的多行计算式。
func renderDetail(c *model.Component, ts *spec.TypeSpec, known bool, table *spec.Table, opt Options, rep *Report) string {
	lines := []string{}
	used := map[string]bool{}

	emit := func(q spec.Qty, qty model.Quantity) {
		line := render.Line(q.Name, q.Raw, qty.JSS.String(), qty.JSL.String(), q.Unit, q.Suffix, qty.JSSJG.String())
		lines = append(lines, line)
		used[q.Code] = true
	}

	if known {
		for _, item := range ts.Items {
			if item.Code == spec.CodeQuantity {
				if opt.EmitQuantity && ts.QtyUnit != "" {
					if l := render.QuantityLine(ts.QtyUnit); l != "" {
						lines = append(lines, l)
					}
				}
				continue
			}
			if qty, ok := c.Quantity(item.Code); ok {
				emit(item, qty)
			}
		}
	} else {
		// 未收录子类：按数据中原顺序输出，名称与单位取兜底表。
		for _, qty := range c.Quantities {
			emit(qtyFromQuantity(qty.LX, qty), qty)
		}
	}

	// 统计规则外的编码
	for _, qty := range c.Quantities {
		if _, _, _, ok := spec.CodeMeta(qty.LX); !ok {
			rep.UnmappedCodes[qty.LX]++
			rep.warnf("工程量编码 %s 未收录，已按 计算式/单位后缀 兜底渲染", qty.LX)
		} else if !used[qty.LX] {
			rep.HiddenCodes[qty.LX]++
		}
	}

	if opt.AppendExtra {
		for _, qty := range c.Quantities {
			if used[qty.LX] {
				continue
			}
			emit(qtyFromQuantity(qty.LX, qty), qty)
		}
	}
	if len(lines) == 0 {
		return typeNameFor(ts, known) + "：无工程量数据"
	}
	return strings.Join(lines, "\n")
}

func typeNameFor(ts *spec.TypeSpec, known bool) string {
	if known && ts != nil {
		return ts.Name
	}
	return "构件"
}

// fallbackQty 为未收录到规则表的编码生成兜底定义。
func fallbackQty(code string) spec.Qty {
	name, unit, suffix, ok := spec.CodeMeta(code)
	if ok {
		return spec.Qty{Code: code, Name: name, Raw: name, Unit: unit, Suffix: suffix}
	}
	return spec.Qty{Code: code, Name: code, Raw: code, Unit: spec.UnitLength, Suffix: ""}
}

// qtyFromQuantity 生成兜底定义：优先用内置编码表，其次从 JSSJG 的单位后缀反推。
func qtyFromQuantity(code string, q model.Quantity) spec.Qty {
	item := fallbackQty(code)
	if item.Suffix != "" {
		return item
	}
	if unit, suffix, ok := spec.UnitFromResultSuffix(q.JSSJG.String()); ok {
		item.Unit, item.Suffix = unit, suffix
	}
	if item.Suffix == "" {
		item.Suffix = "m"
	}
	return item
}

// LoadPositions 读取「CID ↔ 图元位置」对照文件：首列为 CID、第二列为位置。
//
// 用标准 CSV 解析而不是按逗号切分 —— 因为「图元位置」本身就形如 `<6,D>`，
// 含逗号，只有在字段被引号包裹时才能正确还原。文件里出现制表符时按 TSV 解析；
// 首行的 CID/ID 表头会自动忽略。
func LoadPositions(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取位置文件失败: %w", err)
	}
	text := strings.TrimPrefix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\ufeff")
	out := map[string]string{}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	comma := ','
	if strings.ContainsRune(text, '\t') {
		comma = '\t'
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	for line := 1; ; line++ {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析位置文件第 %d 行失败: %w", line, err)
		}
		if len(rec) < 2 {
			continue
		}
		key := strings.TrimSpace(rec[0])
		// 该文件只有两列；把第一列之后的全部内容都视为位置，
		// 这样即使位置值 `<6,D>` 没加引号（被逗号切开）也能还原。
		val := strings.TrimSpace(strings.Join(rec[1:], string(comma)))
		if line == 1 && (strings.EqualFold(key, "cid") || strings.EqualFold(key, "id")) {
			continue
		}
		if key != "" {
			out[key] = val
		}
	}
	return out, nil
}
