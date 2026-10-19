package build

import (
	"os"
	"strings"
	"testing"

	"github.com/ninglang/zhiduoxing-xls/internal/model"
	"github.com/ninglang/zhiduoxing-xls/internal/render"
	"github.com/ninglang/zhiduoxing-xls/internal/spec"
	"github.com/ninglang/zhiduoxing-xls/internal/xls"
)

// sampleDoc 构造一份与智多星导出结构一致的最小数据：
// 首层两个「柱」图元（同一 ZL+MC 归为一组）与一个「梁」图元。
func sampleDoc() (*model.Document, *spec.Table) {
	doc := &model.Document{}
	doc.Norm = model.Section{
		Project: model.Project{
			SubProject: []model.SubProject{{
				Floor: []model.Floor{{
					LCH: 1,
					Components: []model.Component{
						{
							CID: "c1", LX: "GJ_Z", ZL: "GJ_Z_TZ", MC: "KZ9",
							Quantities: []model.Quantity{
								{LX: "GCL_ZC", JSS: "((0.4【长度】+0.4【宽度】)*2)", JSL: "1600", JSSJG: "=1.600m"},
								{LX: "GCL_TJ", JSS: "(0.4【长度】*0.4【宽度】*4.5【高度】)", JSL: "720000000", JSSJG: "=0.720m³"},
							},
						},
						{
							CID: "c2", LX: "GJ_Z", ZL: "GJ_Z_TZ", MC: "KZ9",
							Quantities: []model.Quantity{
								{LX: "GCL_ZC", JSS: "((0.4【长度】+0.4【宽度】)*2)", JSL: "1600", JSSJG: "=1.600m"},
								{LX: "GCL_TJ", JSS: "(0.4【长度】*0.4【宽度】*4.5【高度】)", JSL: "720000000", JSSJG: "=0.720m³"},
							},
						},
						{
							CID: "c3", LX: "GJ_L", ZL: "GJ_L_L", MC: "KL1",
							Quantities: []model.Quantity{
								{LX: "GCL_TJ", JSS: "(0.2【长度】*0.5【宽度】*6【高度】)", JSL: "600000000", JSSJG: "=0.600m³"},
							},
						},
					},
				}},
			}},
		},
	}
	return doc, spec.NewTable()
}

func TestConvertLayout(t *testing.T) {
	doc, table := sampleDoc()
	wb, rep, err := Convert(doc, table, Options{EmitQuantity: true})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	sh := wb.Sheet

	if rep.Components != 3 || rep.Groups != 2 {
		t.Fatalf("统计 = 图元 %d / 分组 %d，期望 3 / 2", rep.Components, rep.Groups)
	}
	if rep.Floor != "首层" || rep.SheetName != "绘图输入构件工程量计算书-首层" {
		t.Fatalf("楼层/工作表名 = %q / %q", rep.Floor, rep.SheetName)
	}

	// 表头
	for c, want := range []string{"序号", "图元位置", "工程量计算式"} {
		cell, ok := sh.CellAt(0, c)
		if !ok || cell.Value() != want {
			t.Errorf("表头 (%d,0) = %q，期望 %q", c, cell.Value(), want)
		}
	}
	if cell, _ := sh.CellAt(0, 0); cell.Style() != xls.XfHeaderLeft {
		t.Errorf("表头首列样式 = %d，期望 %d", cell.Style(), xls.XfHeaderLeft)
	}
	if cell, _ := sh.CellAt(0, 2); cell.Style() != xls.XfHeaderRight {
		t.Errorf("表头末列样式 = %d，期望 %d", cell.Style(), xls.XfHeaderRight)
	}

	// 构件标题行：合并区内的每一格都写入同一段文本（与参照文件一致）。
	title := "首层 - 柱 - KZ9"
	for c := 0; c < 3; c++ {
		cell, ok := sh.CellAt(1, c)
		if !ok || cell.Value() != title {
			t.Errorf("构件标题行第 %d 列 = %q，期望 %q", c, cell.Value(), title)
		}
	}

	// 图元行：序号列与图元位置列竖向合并两行，两行的值相同。
	for _, pair := range [][2]int{{2, 3}, {4, 5}} {
		a, _ := sh.CellAt(pair[0], 0)
		b, _ := sh.CellAt(pair[1], 0)
		if !a.IsNumber() || !b.IsNumber() || a.Value() != b.Value() {
			t.Errorf("第 %d/%d 行的序号应同为数值且相等，得到 %q / %q", pair[0], pair[1], a.Value(), b.Value())
		}
	}

	// 行高：标题行与「柱: xx」行固定 285，明细行按内容自动换行推算。
	if got := sh.Height(1); got != render.RowHeightBase {
		t.Errorf("标题行行高 = %d，期望 %d", got, render.RowHeightBase)
	}
	if got := sh.Height(2); got != render.RowHeightBase {
		t.Errorf("图元首行行高 = %d，期望 %d", got, render.RowHeightBase)
	}
	detail, _ := sh.CellAt(3, 2)
	want := render.RowHeight(render.DisplayLines(detail.Value(), render.CellCapacity))
	if got := sh.Height(3); got != want {
		t.Errorf("明细行行高 = %d，期望 %d", got, want)
	}

	// 合并区：每个构件分组 1 个标题行合并（共 2 组），
	// 再加每个图元的「序号」「图元位置」两处竖向合并（共 3 个图元）。
	const wantMerges = 2 + 2*3
	if len(sh.Merges) != wantMerges {
		t.Fatalf("合并区数量 = %d，期望 %d", len(sh.Merges), wantMerges)
	}
	if m := sh.Merges[0]; m != (xls.Merge{RowFirst: 1, RowLast: 1, ColFirst: 0, ColLast: 2}) {
		t.Errorf("首个合并区 = %+v", m)
	}

	// 全表最后一行换用「下框线中粗」的收口样式。
	lastRow := len(sh.Rows) - 1
	for c, wantStyle := range []int{xls.XfLastSerial, xls.XfLastPosition, xls.XfLastDetail} {
		cell, ok := sh.CellAt(lastRow, c)
		if !ok || cell.Style() != wantStyle {
			t.Errorf("末行第 %d 列样式 = %d，期望 %d", c, cell.Style(), wantStyle)
		}
	}
}

func TestDetailContent(t *testing.T) {
	doc, table := sampleDoc()
	wb, _, err := Convert(doc, table, Options{EmitQuantity: true})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	sh := wb.Sheet
	name, _ := sh.CellAt(2, 2)
	if name.Value() != "柱: KZ9" {
		t.Errorf("图元首行 = %q，期望「柱: KZ9」", name.Value())
	}
	detail, _ := sh.CellAt(3, 2)
	got := detail.Value()
	// 计算式里的 【】 要变成 <>，数字要规范化（1.600m → 1.6m）。
	if !strings.Contains(got, "周长 = ((0.4<长度>+0.4<宽度>)*2) = 1.6m") {
		t.Errorf("周长行渲染不符: %q", got)
	}
	if !strings.Contains(got, "体积 = (0.4<长度>*0.4<宽度>*4.5<高度>) = 0.72m3") {
		t.Errorf("体积行渲染不符: %q", got)
	}
	// 数量行：图元即单个构件，数量恒为 1。
	if !strings.Contains(got, "数量 = 1 = 1根") {
		t.Errorf("缺少数量行: %q", got)
	}
	if strings.Contains(got, "【") || strings.Contains(got, "】") {
		t.Errorf("仍存在未替换的书名号: %q", got)
	}
}

func TestSkippedAndUnknownCodes(t *testing.T) {
	doc, table := sampleDoc()
	// 板负筋在参照文件中没有对应量名，应被跳过。
	sec := &doc.Norm
	sec.Project.SubProject[0].Floor[0].Components = append(
		sec.Project.SubProject[0].Floor[0].Components,
		model.Component{CID: "c4", ZL: "GJ_B_BFJ", MC: "板负筋",
			Quantities: []model.Quantity{{LX: "GCL_JSJMJ", JSL: "1000", JSSJG: "=1m"}}},
	)
	_, rep, err := Convert(doc, table, Options{EmitQuantity: true})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(rep.Skipped) == 0 {
		t.Error("板负筋应被计入跳过统计")
	}
	if rep.Components != 4 {
		t.Errorf("图元总数 = %d，期望 4", rep.Components)
	}

	// 打开 AppendExtra 后，规则外的工程量编码应被输出而不是丢弃。
	wb, rep2, err := Convert(doc, table, Options{Section: "norm", AppendExtra: true, EmitQuantity: true})
	if err != nil {
		t.Fatalf("Convert(AppendExtra): %v", err)
	}
	if len(rep2.HiddenCodes) != 0 {
		t.Errorf("AppendExtra 打开后不应再有未输出的编码: %v", rep2.HiddenCodes)
	}
	_ = wb
}

func TestPositionsFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/pos.csv"
	content := "CID,位置\nc1,\"<6,D>\"\nc2,<7,D>\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时文件: %v", err)
	}
	m, err := LoadPositions(path)
	if err != nil {
		t.Fatalf("LoadPositions: %v", err)
	}
	if m["c1"] != "<6,D>" || m["c2"] != "<7,D>" {
		t.Fatalf("解析结果 = %v", m)
	}
	if _, ok := m["CID"]; ok {
		t.Error("表头行应被忽略")
	}
}
