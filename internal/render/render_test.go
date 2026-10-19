package render

import (
	"testing"

	"github.com/ninglang/zhiduoxing-xls/internal/spec"
)

func TestNormalizeLiteral(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.5000000000", "0.5"},
		{"4", "4"},
		{"2.0000", "2"},
		{"0.12345", "0.1235"}, // 四舍五入到 4 位小数
		{"0.00004", "0"},      // 不足半个末位
		{"0.00005", "0.0001"},
		{"-1.2500", "-1.25"},
	}
	for _, c := range cases {
		if got := NormalizeLiteral(c.in); got != c.want {
			t.Errorf("NormalizeLiteral(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestValueScaling(t *testing.T) {
	// JSL 以毫米为基准：长度 mm、面积 mm²、体积 mm³、数量为原始整数。
	cases := []struct {
		jsl  int64
		unit spec.Unit
		want string
	}{
		{2000, spec.UnitLength, "2"},
		{1600000, spec.UnitArea, "1.6"},
		{720000000, spec.UnitVolume, "0.72"},
		{1, spec.UnitCount, "1"},
		{6340000, spec.UnitArea, "6.34"},
		{-4500, spec.UnitLength, "-4.5"},
	}
	for _, c := range cases {
		if got := Value(c.jsl, c.unit); got != c.want {
			t.Errorf("Value(%d, %v) = %q，期望 %q", c.jsl, c.unit, got, c.want)
		}
	}
}

func TestFormulaAndResult(t *testing.T) {
	if got := Formula("(0.5【长度】*0.5【宽度】)"); got != "(0.5<长度>*0.5<宽度>)" {
		t.Errorf("Formula 书名号替换失败: %q", got)
	}
	if got := Formula("((0.5000000000【长度】+0.5【宽度】)*2)"); got != "((0.5<长度>+0.5<宽度>)*2)" {
		t.Errorf("Formula 数字规范化失败: %q", got)
	}
	if got := Result("= 0.72m³"); got != "0.72m3" {
		t.Errorf("Result = %q，期望 0.72m3", got)
	}
}

func TestLine(t *testing.T) {
	got := Line("周长", "周长", "((0.5<长度>+0.5<宽度>)*2)", "2000", spec.UnitLength, "m", "= 2m")
	if want := "周长 = ((0.5<长度>+0.5<宽度>)*2) = 2m"; got != want {
		t.Errorf("Line = %q，期望 %q", got, want)
	}
	// 计算式为空时退化为「值<原始量标签>」。
	got = Line("模板面积", "原始模板面积", "", "6340000", spec.UnitArea, "m2", "6.34m2")
	if want := "模板面积 = 6.34<原始模板面积> = 6.34m2"; got != want {
		t.Errorf("Line(兜底) = %q，期望 %q", got, want)
	}
	if got := QuantityLine("根"); got != "数量 = 1 = 1根" {
		t.Errorf("QuantityLine = %q", got)
	}
}

func TestVisualWidthAndDisplayLines(t *testing.T) {
	// 「柱」按全角记 2，其余 5 个半角字符各记 1。
	if got := VisualWidth("柱: KZ9"); got != 7 {
		t.Errorf("VisualWidth(柱: KZ9) = %d，期望 7", got)
	}
	if got := VisualWidth("长度"); got != 4 {
		t.Errorf("VisualWidth(长度) = %d，期望 4", got)
	}
	// 容量 79 是依据真实文件标定的列宽容量。
	if got := DisplayLines("", CellCapacity); got != 1 {
		t.Errorf("DisplayLines(空) = %d，期望 1", got)
	}
	if got := DisplayLines("a\nb\nc", CellCapacity); got != 3 {
		t.Errorf("DisplayLines(3 行) = %d，期望 3", got)
	}
	long := make([]rune, 0, 160)
	for i := 0; i < 160; i++ {
		long = append(long, 'a')
	}
	if got := DisplayLines(string(long), CellCapacity); got != 3 {
		t.Errorf("DisplayLines(160 半角字符) = %d，期望 3", got)
	}
}

func TestRowHeight(t *testing.T) {
	cases := []struct{ lines, want int }{
		{1, 285},
		{2, 495},
		{7, 1545},
		{0, 285},
	}
	for _, c := range cases {
		if got := RowHeight(c.lines); got != c.want {
			t.Errorf("RowHeight(%d) = %d，期望 %d", c.lines, got, c.want)
		}
	}
}
