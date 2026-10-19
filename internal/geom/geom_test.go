package geom

import (
	"math"
	"testing"

	"github.com/nn824/Tobiao/internal/grid"
)

// 一份最小模型：柱（点）、门（点 + 洞口宽）、墙（线）、三跨梁（3 条线）、
// 被截断的过梁（线 + 设计长度 CD）。
const sample = `{
  "ZGCS": [ { "LCS": [ { "component": [
    {
      "@type": "type.googleapis.com/pb.ConcreteColumnRectangle",
      "concreteColumn": {
        "component": {"cid": "cid-col", "pid": "pid-col", "name": "KZ1"},
        "pointComponent": {"matrix": [1,0,0,34100, 0,1,0,0, 0,0,1,-21600, 0,0,0,1]}
      }
    },
    {
      "@type": "type.googleapis.com/pb.MasonryWallRectangle",
      "wall": {
        "component": {"cid": "cid-wall", "pid": "pid-wall", "name": "QTQ"},
        "line": {"start": {"x": 60000, "y": 0, "z": -21600},
                 "end":   {"x": 70000, "y": 0, "z": -21600}}
      }
    },
    {
      "@type": "type.googleapis.com/pb.RectDoor",
      "DKKD": "1100",
      "Door": {
        "component": {"cid": "cid-door", "pid": "pid-door", "name": "M1"},
        "pointComponent": {"matrix": [1,0,0,65500, 0,1,0,0, 0,0,1,-21600, 0,0,0,1]}
      }
    },
    {
      "@type": "type.googleapis.com/pb.BeamRectangle",
      "beam": {
        "component": {"cid": "cid-beam", "pid": "pid-beam", "name": "KL1(3)"},
        "KS": [
          {"KH": "1", "path": [{"line": {"start": {"x": 0, "y": 0, "z": -30000},
                                          "end":   {"x": 9900, "y": 0, "z": -30000}}}]},
          {"KH": "2", "path": [{"line": {"start": {"x": 9900, "y": 0, "z": -30000},
                                          "end":   {"x": 18900, "y": 0, "z": -30000}}}]},
          {"KH": "3", "path": [{"line": {"start": {"x": 18900, "y": 0, "z": -30000},
                                          "end":   {"x": 27900, "y": 0, "z": -30000}}}]}
        ]
      }
    },
    {
      "@type": "type.googleapis.com/pb.LintelRect",
      "lintel": {
        "component": {"cid": "cid-gl", "pid": "pid-gl", "name": "GL1"},
        "line": {"start": {"x": 65810.12, "y": 0, "z": -4500},
                 "end":   {"x": 67200,    "y": 0, "z": -4500}},
        "CD": "1530"
      }
    }
  ] } ] } ]
}`

func mustParse(t *testing.T) *Model {
	t.Helper()
	m, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("解析样例模型失败: %v", err)
	}
	if m.Count() != 5 {
		t.Fatalf("样例应有 5 个图元，实际 %d", m.Count())
	}
	return m
}

func TestResolveByPID(t *testing.T) {
	r := mustParse(t).Resolver(nil)
	cases := map[string]string{
		"pid-col":  "<6,D>",
		"pid-wall": "<9+3500,D><10+4500,D>",
		"pid-beam": "<1,E><4,E>",
		"pid-gl":   "<10+310,B><10+1840,B>",
	}
	for pid, want := range cases {
		if got := r.Position("", pid); got != want {
			t.Errorf("PID %s → %s，期望 %s", pid, got, want)
		}
	}
}

func TestDoorOffsetUsesHostWall(t *testing.T) {
	r := mustParse(t).Resolver(nil)
	// 门在沿 x 的墙上，洞口宽 1100 → 沿 x 偏移 550：65500+550=66050
	if got := r.Position("", "pid-door"); got != "<10+550,D>" {
		t.Errorf("门位置 = %s，期望 <10+550,D>", got)
	}
}

func TestMultiSpanBeamIsCollapsedToEnds(t *testing.T) {
	r := mustParse(t).Resolver(nil)
	got := r.Position("", "pid-beam")
	if got != "<1,E><4,E>" {
		t.Errorf("三跨梁应只写起止两点，得到 %s", got)
	}
}

func TestLintelStretchedToDesignLength(t *testing.T) {
	r := mustParse(t).Resolver(nil)
	// 模型里的线长 1389.88，设计长度 CD=1530，应沿原方向补到 67340.12
	if got := r.Position("", "pid-gl"); got != "<10+310,B><10+1840,B>" {
		t.Errorf("过梁位置 = %s，期望 <10+310,B><10+1840,B>", got)
	}
}

func TestUnknownPIDFallsBackToCID(t *testing.T) {
	r := mustParse(t).Resolver(nil)
	// 找不到 PID 时按 CID 分组、按调用顺序取值
	if got := r.Position("cid-col", "nope"); got != "<6,D>" {
		t.Errorf("CID 兜底 = %s，期望 <6,D>", got)
	}
	if got := r.Position("no-such-cid", ""); got != "" {
		t.Errorf("未收录的构件应返回空串，得到 %s", got)
	}
}

func TestStretchTo(t *testing.T) {
	base := []grid.Point{{X: 0, Z: 0}, {X: 1000, Z: 0}}
	if got := stretchTo(base, 0); len(got) != 2 || got[1].X != 1000 {
		t.Error("没有设计长度时不应改动线段")
	}
	if got := stretchTo(base, 900); got[1].X != 1000 {
		t.Error("线段已经够长时不应缩短")
	}
	got := stretchTo(base, 2000)
	if got[0].X != 0 || got[1].X != 2000 {
		t.Errorf("应沿原方向补到 2000，得到 %v", got)
	}
	// 斜向线段按比例放大
	diag := []grid.Point{{X: 0, Z: 0}, {X: 300, Z: -400}}
	got = stretchTo(diag, 1000)
	if math.Abs(got[1].X-600) > 1e-6 || math.Abs(got[1].Z+800) > 1e-6 {
		t.Errorf("斜向补足错误：%v", got)
	}
}
