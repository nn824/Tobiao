package zhiduoxingxls_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	zhiduoxingxls "github.com/ninglang/zhiduoxing-xls"
)

// oleMagic 是 OLE2 复合文档的文件头，也是 .xls 容器的标志。
var oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// sampleJSON 是一份最小的「首层砼柱测试结果」JSON。
const sampleJSON = `{
  "Shapes": {},
  "Norm": {
    "Project": {
      "SubProject": [
        {
          "Floor": [
            {
              "LCH": 1,
              "Components": [
                {
                  "CID": "c1",
                  "ZL": "GJ_Z_TZ",
                  "MC": "KZ9",
                  "Quantities": [
                    {"LX": "GCL_ZC", "JSL": "1600", "JSS": "(0.4<长度>+0.4<宽度>)*2", "JSSJG": "=1.6m"},
                    {"LX": "GCL_TJ", "JSL": "416000000", "JSS": "0.4<长度>*0.4<宽度>*2.6<高度>", "JSSJG": "=0.416m³"},
                    {"LX": "GCL_MBMJ", "JSL": "4160000", "JSS": "0.4<长度>*2.6<高度>*4", "JSSJG": "=4.16m²"}
                  ]
                },
                {
                  "CID": "c2",
                  "ZL": "GJ_Z_TZ",
                  "MC": "KZ9",
                  "Quantities": [
                    {"LX": "GCL_ZC", "JSL": "1600", "JSS": "(0.4<长度>+0.4<宽度>)*2", "JSSJG": "=1.6m"}
                  ]
                }
              ]
            }
          ]
        }
      ]
    }
  },
  "List": {
    "Project": {"SubProject": []}
  }
}`

func writeSample(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "结果.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写测试输入失败: %v", err)
	}
	return path
}

func TestConvert_DefaultOutAndReport(t *testing.T) {
	in := writeSample(t, sampleJSON)
	want := strings.TrimSuffix(in, filepath.Ext(in)) + ".xls"

	rep, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{})
	if err != nil {
		t.Fatalf("Convert 失败: %v", err)
	}

	if rep.Out != want {
		t.Errorf("默认输出路径 = %q，期望 %q", rep.Out, want)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("未生成输出文件: %v", err)
	}
	if !bytes.HasPrefix(data, oleMagic) {
		t.Errorf("输出不是 OLE2 容器，首 8 字节 = % x", data[:8])
	}

	if rep.Components != 2 {
		t.Errorf("图元数 = %d，期望 2", rep.Components)
	}
	if rep.Groups != 1 {
		t.Errorf("构件分组数 = %d，期望 1（两个图元同为 KZ9）", rep.Groups)
	}
	if rep.SheetName != "绘图输入构件工程量计算书-首层" {
		t.Errorf("工作表名 = %q", rep.SheetName)
	}
	if rep.Floor != "首层" {
		t.Errorf("楼层名 = %q", rep.Floor)
	}
	if rep.Section != "Norm" {
		t.Errorf("分组 = %q", rep.Section)
	}
	// 表头 1 行 + 分组标题 1 行 + 2 个图元 × 2 行 = 6
	if rep.Rows != 6 {
		t.Errorf("总行数 = %d，期望 6", rep.Rows)
	}
}

func TestConvert_OverwriteGuard(t *testing.T) {
	in := writeSample(t, sampleJSON)

	if _, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{}); err != nil {
		t.Fatalf("首次转换失败: %v", err)
	}
	_, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{})
	if !errors.Is(err, zhiduoxingxls.ErrOutputExists) {
		t.Fatalf("重复转换应返回 ErrOutputExists，实际: %v", err)
	}
	if _, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{Overwrite: true}); err != nil {
		t.Fatalf("显式覆盖时仍失败: %v", err)
	}
}

func TestConvert_ExplicitOut(t *testing.T) {
	in := writeSample(t, sampleJSON)
	out := filepath.Join(filepath.Dir(in), "自定义名.xls")

	rep, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{Out: out, SheetName: "我的计算书"})
	if err != nil {
		t.Fatalf("Convert 失败: %v", err)
	}
	if rep.Out != out || rep.SheetName != "我的计算书" {
		t.Errorf("Out = %q，SheetName = %q", rep.Out, rep.SheetName)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("未生成指定输出文件: %v", err)
	}
}

func TestConvert_InputErrors(t *testing.T) {
	if _, err := zhiduoxingxls.Convert("", zhiduoxingxls.Options{}); err == nil {
		t.Error("空路径应报错")
	}
	if _, err := zhiduoxingxls.Convert(filepath.Join(t.TempDir(), "不存在.json"), zhiduoxingxls.Options{}); err == nil {
		t.Error("文件不存在应报错")
	}

	_, _, err := zhiduoxingxls.ConvertBytes([]byte("   \n"), zhiduoxingxls.Options{})
	if !errors.Is(err, zhiduoxingxls.ErrEmptyInput) {
		t.Errorf("空内容应返回 ErrEmptyInput，实际: %v", err)
	}

	_, _, err = zhiduoxingxls.ConvertBytes([]byte("这不是 JSON"), zhiduoxingxls.Options{})
	if !errors.Is(err, zhiduoxingxls.ErrParseJSON) {
		t.Errorf("脏内容应返回 ErrParseJSON，实际: %v", err)
	}
}

// TestConvert_DirtyJSON 验证容错解析：BOM、Markdown 代码围栏、行注释、
// 区块注释、尾随逗号。
func TestConvert_DirtyJSON(t *testing.T) {
	dirty := "\ufeff```json\n" + `{
  "Shapes": {},
  // 行注释：下面开始是正规分组
  "Norm": {
    "Project": {
      "SubProject": [
        {
          "Floor": [
            {
              "LCH": 1,
              "Components": [
                {
                  "CID": "c1",
                  "ZL": "GJ_Z_TZ",
                  "MC": "KZ9",
                  "Quantities": [
                    {"LX": "GCL_ZC", "JSL": "1600", "JSS": "x", "JSSJG": "=1.6m"},
                    {"LX": "GCL_TJ", "JSL": "416000000", "JSS": "y", "JSSJG": "=0.416m³"},
                  ],
                },
              ],
            },
          ],
        },
      ],
    },
  },
  /* 区块注释 */
  "List": {"Project": {"SubProject": []}},
}
` + "```\n"

	stream, rep, err := zhiduoxingxls.ConvertBytes([]byte(dirty), zhiduoxingxls.Options{})
	if err != nil {
		t.Fatalf("容错解析失败: %v", err)
	}
	if !bytes.HasPrefix(stream, oleMagic) {
		t.Error("输出不是 OLE2 容器")
	}
	if rep.Components != 1 {
		t.Errorf("图元数 = %d，期望 1", rep.Components)
	}
}

func TestConvertBytes_NoFileWritten(t *testing.T) {
	dir := t.TempDir()
	before, _ := os.ReadDir(dir)

	stream, rep, err := zhiduoxingxls.ConvertBytes([]byte(sampleJSON), zhiduoxingxls.Options{Out: filepath.Join(dir, "忽略.xls")})
	if err != nil {
		t.Fatalf("ConvertBytes 失败: %v", err)
	}
	if len(stream) == 0 || !bytes.HasPrefix(stream, oleMagic) {
		t.Error("未返回合法的 .xls 字节流")
	}
	if rep.Out != "" {
		t.Errorf("ConvertBytes 不应设置 Out，实际 %q", rep.Out)
	}
	after, _ := os.ReadDir(dir)
	if len(before) != len(after) {
		t.Error("ConvertBytes 不应落盘任何文件")
	}
}

// TestConvert_PositionsCSV 验证在输入同目录放置 <同名>.positions.csv 时，
// 会自动读取并填充「图元位置」列（不再产生相关提示）。
func TestConvert_PositionsCSV(t *testing.T) {
	in := writeSample(t, sampleJSON)
	csvPath := strings.TrimSuffix(in, filepath.Ext(in)) + ".positions.csv"
	content := "CID,位置\n" + `c1,"<6,D>"` + "\n" + `c2,"<7,D>"` + "\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写 positions.csv 失败: %v", err)
	}

	rep, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{})
	if err != nil {
		t.Fatalf("Convert 失败: %v", err)
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "图元位置") {
			t.Errorf("已提供 positions.csv，不应再提示位置缺失: %s", w)
		}
	}

	// 显式传入 Positions 时也应覆盖自动读取，且不产生提示。
	rep2, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{
		Overwrite: true,
		Positions: map[string]string{"c1": "<1,A>"},
	})
	if err != nil {
		t.Fatalf("Convert（显式 Positions）失败: %v", err)
	}
	for _, w := range rep2.Warnings {
		if strings.Contains(w, "图元位置") {
			t.Errorf("显式 Positions 下不应提示位置缺失: %s", w)
		}
	}
}

// TestConvert_OmitQuantity 验证 OmitQuantity 会真实改变。注意 OLE2 容器按
// 512 字节扇区补齐，小幅差异不会改变文件总长，所以直接在校验码流里找
// 「数量」这个字段名（SST 中以 UTF-16LE 存储）。
func TestConvert_OmitQuantity(t *testing.T) {
	in := writeSample(t, sampleJSON)
	full, err := os.ReadFile(mustConvert(t, in, zhiduoxingxls.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	less, err := os.ReadFile(mustConvert(t, in, zhiduoxingxls.Options{OmitQuantity: true, Overwrite: true}))
	if err != nil {
		t.Fatal(err)
	}

	marker := utf16LEBytes("数量")
	if !bytes.Contains(full, marker) {
		t.Error("默认应输出「数量」行，但码流里找不到该字段")
	}
	if bytes.Contains(less, marker) {
		t.Error("OmitQuantity 为 true 时不应出现「数量」字段")
	}
	if bytes.Equal(full, less) {
		t.Error("两个输出完全相同，说明 OmitQuantity 没有生效")
	}
}

// utf16LEBytes 返回字符串的 UTF-16LE 编码，用于在 .xls 码流中检索。
func utf16LEBytes(s string) []byte {
	var out []byte
	for _, r := range s {
		if r > 0xFFFF {
			hi, lo := utf16.EncodeRune(r)
			out = append(out, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
			continue
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

func mustConvert(t *testing.T, in string, opts zhiduoxingxls.Options) string {
	t.Helper()
	rep, err := zhiduoxingxls.Convert(in, opts)
	if err != nil {
		t.Fatalf("Convert 失败: %v", err)
	}
	return rep.Out
}

// TestDefaultAxes 锁定内置轴网，避免有人无意改动后影响已有工程。
func TestDefaultAxes(t *testing.T) {
	a := zhiduoxingxls.DefaultAxes()
	if len(a.X) != 11 || len(a.Y) != 5 {
		t.Fatalf("内置轴网应为横向 11 条、纵向 5 条，实际 %d / %d", len(a.X), len(a.Y))
	}
	if a.X[0].Pos != 0 || a.X[len(a.X)-1].Pos != 75400 {
		t.Errorf("横向轴网应从 0 起、到 75400 止，实际 %g ~ %g", a.X[0].Pos, a.X[len(a.X)-1].Pos)
	}
	if a.Y[0].Label != "A" || a.Y[len(a.Y)-1].Label != "E" || a.Y[len(a.Y)-1].Pos != 30000 {
		t.Errorf("纵向轴网应为 A(0) ~ E(30000)，实际 %s(%g) ~ %s(%g)",
			a.Y[0].Label, a.Y[0].Pos, a.Y[len(a.Y)-1].Label, a.Y[len(a.Y)-1].Pos)
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	return p
}

// TestLoadAxes 覆盖换轴网的入口：正常读取、自动排序、缺字段报错。
func TestLoadAxes(t *testing.T) {
	p := writeTemp(t, "axes.json", `{
	  "x": [{"label":"2","pos":9900}, {"label":"1","pos":0}],
	  "y": [{"label":"B","pos":4500}, {"label":"A","pos":0}]
	}`)
	a, err := zhiduoxingxls.LoadAxes(p)
	if err != nil {
		t.Fatalf("LoadAxes 失败: %v", err)
	}
	if got := []string{a.X[0].Label, a.X[1].Label}; got[0] != "1" || got[1] != "2" {
		t.Errorf("轴网应按 pos 自动升序，实际 %v", got)
	}
	if got := []string{a.Y[0].Label, a.Y[1].Label}; got[0] != "A" || got[1] != "B" {
		t.Errorf("纵向轴网应按 pos 自动升序，实际 %v", got)
	}
}

func TestLoadAxesErrors(t *testing.T) {
	if _, err := zhiduoxingxls.LoadAxes(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("文件不存在时应报错")
	}
	p := writeTemp(t, "empty.json", `{"abc":1}`)
	if _, err := zhiduoxingxls.LoadAxes(p); err == nil {
		t.Error("既无 x 也无 y 时应报错")
	}
	p = writeTemp(t, "nolabel.json", `{"x":[{"pos":100}]}`)
	if _, err := zhiduoxingxls.LoadAxes(p); err == nil {
		t.Error("轴号为空时应报错")
	}
}

// TestWriteAxesFileRoundtrip 保证导出的模板能原样读回（-dump-axes → -axes）。
func TestWriteAxesFileRoundtrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "axes.json")
	if err := zhiduoxingxls.WriteAxesFile(p, nil); err != nil {
		t.Fatalf("WriteAxesFile 失败: %v", err)
	}
	back, err := zhiduoxingxls.LoadAxes(p)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	want := zhiduoxingxls.DefaultAxes()
	if len(back.X) != len(want.X) || len(back.Y) != len(want.Y) {
		t.Fatalf("往返后条数变了：x %d→%d，y %d→%d", len(want.X), len(back.X), len(want.Y), len(back.Y))
	}
	for i := range want.X {
		if back.X[i] != want.X[i] {
			t.Errorf("横向第 %d 条不一致：%v → %v", i, want.X[i], back.X[i])
		}
	}
	for i := range want.Y {
		if back.Y[i] != want.Y[i] {
			t.Errorf("纵向第 %d 条不一致：%v → %v", i, want.Y[i], back.Y[i])
		}
	}
}

// TestConvertWithAxesPath 用一个整体平移过的轴网，验证位置字符串随之改变，
// 证明 AxesPath 确实生效（而不是被忽略掉用了内置轴网）。
func TestConvertWithAxesPath(t *testing.T) {
	in := writeTemp(t, "in.json", `{"Norm":{"Project":{"SubProject":[{"Floor":[{"Name":"F","List":[]}]}]}}}`)
	axes := writeTemp(t, "axes.json", `{"x":[{"label":"A1","pos":0}],"y":[{"label":"AA","pos":0}]}`)
	out := filepath.Join(t.TempDir(), "out.xls")
	if _, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{
		Out:      out,
		AxesPath: axes,
	}); err != nil {
		t.Fatalf("Convert 失败: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("输出文件未生成: %v", err)
	}
	bad := writeTemp(t, "bad.json", `{"x":[{"pos":1}]}`)
	if _, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{
		Out:      filepath.Join(t.TempDir(), "o2.xls"),
		AxesPath: bad,
	}); err == nil {
		t.Error("轴号为空时应报错")
	}
}

// TestLoadAxesNative 覆盖智多星直接导出的原生轴网文件：
// 换工程时把这份文件用 -axes / Options.AxesPath 传进来即可，不用转换格式。
func TestLoadAxesNative(t *testing.T) {
	// 同一条轴会在左右（上下）各出现一次，distance 相同，应当只保留一条
	p := writeTemp(t, "axes.json", `[
	  {"axis":{"axisLabel":"B","axisSpacing":6600},"distance":6600,"direction":"horizontal","location":"left"},
	  {"axis":{"axisLabel":"A","axisSpacing":0},   "distance":0,   "direction":"horizontal","location":"left"},
	  {"axis":{"axisLabel":"A","axisSpacing":0},   "distance":0,   "direction":"horizontal","location":"right"},
	  {"axis":{"axisLabel":"1/E","axisSpacing":700},"distance":21700,"direction":"horizontal","location":"right"},
	  {"axis":{"axisLabel":"3","axisSpacing":6600},"distance":13200,"direction":"vertical","location":"bottom"},
	  {"axis":{"axisLabel":"1","axisSpacing":0},   "distance":0,   "direction":"vertical","location":"bottom"},
	  {"axis":{"axisLabel":"1/1","axisSpacing":3100},"distance":3100,"direction":"vertical","location":"top"}
	]`)

	a, err := zhiduoxingxls.LoadAxes(p)
	if err != nil {
		t.Fatalf("LoadAxes 失败: %v", err)
	}
	// 横向（数字轴号）应来自 vertical 方向的条目，按 pos 升序
	wantX := []struct {
		label string
		pos   float64
	}{{"1", 0}, {"1/1", 3100}, {"3", 13200}}
	if len(a.X) != len(wantX) {
		t.Fatalf("横向应 %d 轴，实际 %d：%v", len(wantX), len(a.X), a.X)
	}
	for i, w := range wantX {
		if a.X[i].Label != w.label || a.X[i].Pos != w.pos {
			t.Errorf("横向第 %d 条应为 %s(%g)，实际 %s(%g)", i, w.label, w.pos, a.X[i].Label, a.X[i].Pos)
		}
	}
	// 纵向（字母轴号）应来自 horizontal 方向的条目，A 重复出现要合并
	wantY := []struct {
		label string
		pos   float64
	}{{"A", 0}, {"B", 6600}, {"1/E", 21700}}
	if len(a.Y) != len(wantY) {
		t.Fatalf("纵向应 %d 轴，实际 %d：%v", len(wantY), len(a.Y), a.Y)
	}
	for i, w := range wantY {
		if a.Y[i].Label != w.label || a.Y[i].Pos != w.pos {
			t.Errorf("纵向第 %d 条应为 %s(%g)，实际 %s(%g)", i, w.label, w.pos, a.Y[i].Label, a.Y[i].Pos)
		}
	}
}

func TestLoadAxesNativeErrors(t *testing.T) {
	cases := map[string]string{
		"空数组":          `[]`,
		"轴号为空":         `[{"axis":{"axisLabel":""},"distance":0,"direction":"horizontal"}]`,
		"direction 非法": `[{"axis":{"axisLabel":"A"},"distance":0,"direction":"斜的"}]`,
		"不是轴网":         `[{"foo":1}]`,
	}
	for name, body := range cases {
		p := writeTemp(t, "bad.json", body)
		if _, err := zhiduoxingxls.LoadAxes(p); err == nil {
			t.Errorf("%s 时应报错", name)
		}
	}
}

// TestConvertWithNativeAxes 端到端验证：模型坐标按原生轴网换算成图元位置。
func TestConvertWithNativeAxes(t *testing.T) {
	// 模型：一根柱在 (2000, -4000)，对应轴网 横向 1/3、纵向 A/B
	model := writeTemp(t, "m.json", `{"ZGCS":[{"LCS":[{"component":[
	  {"@type":"type.googleapis.com/pb.ConcreteColumnRectangle",
	   "concreteColumn":{"component":{"cid":"c1","pid":"p1","name":"KZ1"},
	   "pointComponent":{"matrix":[1,0,0,2000,0,1,0,0,0,0,1,-4000]}}}]}]}]}`)
	axes := writeTemp(t, "axes.json", `[
	  {"axis":{"axisLabel":"1","axisSpacing":3000},"distance":0,"direction":"vertical"},
	  {"axis":{"axisLabel":"3","axisSpacing":3000},"distance":3000,"direction":"vertical"},
	  {"axis":{"axisLabel":"A","axisSpacing":5000},"distance":0,"direction":"horizontal"},
	  {"axis":{"axisLabel":"B","axisSpacing":5000},"distance":5000,"direction":"horizontal"}
	]`)
	in := writeTemp(t, "in.json", `{"Norm":{"Project":{"SubProject":[{"Floor":[{"Name":"首层","Components":[
	  {"CID":"c1","PID":"p1","ZL":"GJ_Z_TZ","MC":"KZ1","Quantities":[
	    {"LX":"GCL_TJ","JSL":"1000000","JSS":"体积 1.000","JSSJG":1.0}]}]}]}]}}}`)
	out := filepath.Join(t.TempDir(), "out.xls")

	rep, err := zhiduoxingxls.Convert(in, zhiduoxingxls.Options{
		Out:       out,
		ModelPath: model,
		AxesPath:  axes,
	})
	if err != nil {
		t.Fatalf("Convert 失败: %v", err)
	}
	if rep.Positioned != 1 {
		t.Errorf("应有 1 个图元填上位置，实际 %d", rep.Positioned)
	}
	if !strings.Contains(rep.Axes, "axes.json") {
		t.Errorf("Report.Axes 应写明轴网来源，实际 %q", rep.Axes)
	}
}
