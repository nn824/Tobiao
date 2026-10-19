package compare

import (
	"testing"

	"github.com/ninglang/zhiduoxing-xls/internal/xls"
)

// buildSheet 按「绘图输入构件工程量计算书」的版式造一张小表。
func buildSheet(t *testing.T) *xls.Sheet {
	t.Helper()
	s := &xls.Sheet{Name: "t", ColWidths: []int{1500, 4000, 12000}}
	title := func(r int, v string) {
		s.SetText(r, 0, v, xls.XfGroupLeft)
		s.SetText(r, 1, v, xls.XfGroupLeft)
		s.SetText(r, 2, v, xls.XfGroupRight)
	}
	item := func(r, serial int, pos, name, formula string) {
		s.SetNumber(r, 0, float64(serial), xls.XfSerial)
		s.SetText(r, 1, pos, xls.XfPosition)
		s.SetText(r, 2, name, xls.XfNameRight)
		s.SetNumber(r+1, 0, float64(serial), xls.XfSerial)
		s.SetText(r+1, 1, pos, xls.XfPosition)
		s.SetText(r+1, 2, formula, xls.XfDetailRight)
	}

	title(0, "首层 - 柱 - KZ9")
	item(1, 1, "<6,D>", "柱: KZ9", "体积 = 0.72m3\n周长 = 1.6m")
	item(3, 2, "<7,D>", "柱: KZ9", "体积 = 0.72m3")
	return s
}

func TestParse(t *testing.T) {
	s := buildSheet(t)
	rows := Parse(s, "")
	if len(rows) != 2 {
		t.Fatalf("应解析出 2 个图元，实际 %d", len(rows))
	}
	first := rows[0]
	if first.Floor != "首层" || first.TypeName != "柱" || first.Name != "KZ9" || first.Position != "<6,D>" {
		t.Errorf("第 1 行字段不对: %+v", first)
	}
	if first.Formula != "体积 = 0.72m3\n周长 = 1.6m" {
		t.Errorf("第 1 行计算式不对: %q", first.Formula)
	}
	if rows[1].Position != "<7,D>" || rows[1].Formula != "体积 = 0.72m3" {
		t.Errorf("第 2 行不对: %+v", rows[1])
	}
}

// TestParseFloorOverride 锁定「楼层名按测试结果 JSON 输出」这条需求。
func TestParseFloorOverride(t *testing.T) {
	rows := Parse(buildSheet(t), "二层")
	for _, r := range rows {
		if r.Floor != "二层" {
			t.Fatalf("楼层名应被覆盖为「二层」，实际 %q", r.Floor)
		}
	}
}

func TestCollectStatuses(t *testing.T) {
	gen := []Row{
		{Floor: "首层", TypeName: "柱", Name: "KZ9", Position: "<6,D>", Formula: "A\nB"},
		{Floor: "首层", TypeName: "柱", Name: "KZ9", Position: "<7,D>", Formula: "C"},
		{Floor: "首层", TypeName: "柱", Name: "KZ9", Position: "<8,D>", Formula: "D"},
	}
	ref := []Row{
		// 同一坐标、行序相反，仍算一致
		{Floor: "首层", TypeName: "柱", Name: "KZ9", Position: "<6,D>", Formula: "B\nA"},
		// 同一坐标但内容不同
		{Floor: "首层", TypeName: "柱", Name: "KZ9", Position: "<7,D>", Formula: "C2"},
		// 只在参照文件里
		{Floor: "首层", TypeName: "柱", Name: "KZ9", Position: "<9,D>", Formula: "E"},
	}

	items := Collect(gen, ref)
	st := Count(items)
	if st.Items != 4 {
		t.Fatalf("应有 4 个比对键，实际 %d", st.Items)
	}
	if st.Same != 1 || st.Diff != 1 || st.OnlyGenerated != 1 || st.OnlyReference != 1 {
		t.Fatalf("状态统计不对: %+v", st)
	}

	want := map[string]string{
		"<6,D>": StatusSame,
		"<7,D>": StatusDiff,
		"<8,D>": StatusOnlyGen,
		"<9,D>": StatusOnlyRef,
	}
	for _, it := range items {
		if w := want[it.Position]; w != it.Status {
			t.Errorf("坐标 %s 应为「%s」，实际「%s」", it.Position, w, it.Status)
		}
	}
	// 计算书里的顺序必须保住，参照独有的排在最后。
	if items[0].Position != "<6,D>" || items[len(items)-1].Position != "<9,D>" {
		t.Errorf("顺序不对: 首=%s 尾=%s", items[0].Position, items[len(items)-1].Position)
	}
}

// TestCollectSamePositionTwice 覆盖「同一坐标有多个图元」的集合合并。
func TestCollectSamePositionTwice(t *testing.T) {
	gen := []Row{
		{Floor: "首层", Name: "KZ9", Position: "<6,D>", Formula: "A"},
		{Floor: "首层", Name: "KZ9", Position: "<6,D>", Formula: "B"},
	}
	ref := []Row{
		{Floor: "首层", Name: "KZ9", Position: "<6,D>", Formula: "B"},
		{Floor: "首层", Name: "KZ9", Position: "<6,D>", Formula: "A"},
	}
	items := Collect(gen, ref)
	if len(items) != 1 {
		t.Fatalf("同坐标应合并成 1 条，实际 %d", len(items))
	}
	if items[0].Status != StatusSame {
		t.Errorf("两侧集合相同应为一致，实际 %s", items[0].Status)
	}
	if len(items[0].Generated) != 2 || len(items[0].Reference) != 2 {
		t.Errorf("集合应有 2 个元素，实际 %d / %d", len(items[0].Generated), len(items[0].Reference))
	}
}

func TestBuildProducesOLE2(t *testing.T) {
	data, err := Build([]Item{{
		Key:      Key{Floor: "首层", Name: "KZ9", Position: "<6,D>"},
		TypeName: "柱", Generated: []string{"A"}, Reference: []string{"B"}, Status: StatusDiff,
	}}, WriteOptions{})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(data) < 512 || data[0] != 0xD0 || data[1] != 0xCF {
		t.Fatalf("输出不是 OLE2 复合文档")
	}
}
