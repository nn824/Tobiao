package grid

import "testing"

func TestLabel(t *testing.T) {
	g := Default()
	cases := []struct {
		x, z float64
		want string
	}{
		{34100, -21600, "<6,D>"},
		{27750, -150, "<4-150,A+150>"},
		{67900, -29700, "<10+2400,E-300>"},
		{75900, -25150, "<11+500,D+3550>"},
		{0, 0, "<1,A>"},
	}
	for _, c := range cases {
		if got := g.Label(c.x, c.z); got != c.want {
			t.Errorf("Label(%v, %v) = %s，期望 %s", c.x, c.z, got, c.want)
		}
	}
}

func TestLabelNearestAxis(t *testing.T) {
	g := Default()
	// 轴 5=28300 与轴 6=34100 之间偏向 5 的点
	if got := g.Label(28300, -21600); got != "<5,D>" {
		t.Errorf("轴上应只写轴号，得到 %s", got)
	}
	if got := g.Label(28300.4, -21600); got != "<5,D>" {
		t.Errorf("0.4mm 应舍去，得到 %s", got)
	}
	if got := g.Label(28301.6, -21600); got != "<5+2,D>" {
		t.Errorf("1.6mm 应进位，得到 %s", got)
	}
}

func TestLabels(t *testing.T) {
	g := Default()
	got := g.Labels([]Point{{X: 0, Z: -29950}, {X: 12900, Z: -29950}})
	if got != "<1,E-50><2+3000,E-50>" {
		t.Errorf("Labels = %s", got)
	}
	if g.Labels(nil) != "" {
		t.Error("空点集应返回空字符串")
	}
}

func TestDefaultIsCopy(t *testing.T) {
	a, b := Default(), Default()
	a.X[0].Pos = 999
	if b.X[0].Pos == 999 {
		t.Error("Default 应返回副本，避免调用方改坏内置轴网")
	}
}
