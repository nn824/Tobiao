// Package zhiduoxingxls 把智多星「工程量计算结果」JSON 文件转换成与
// 「绘图输入构件工程量计算书」相同版式的 Excel 97-2003（.xls）文件。
//
// 唯一必填的入参是测试结果 JSON 文件的路径：
//
//	rep, err := zhiduoxingxls.Convert("首层砼柱测试结果.json", zhiduoxingxls.Options{
//		Out:       "首层砼柱计算书.xls",
//		Overwrite: true,
//	})
//	if err != nil {
//		return err
//	}
//	log.Printf("图元 %d 个、构件分组 %d 个 → %s", rep.Components, rep.Groups, rep.Out)
//
// 当 Options.Out 留空时，输出路径自动取「与入参同名的 .xls」，
// 因此最短的调用只需传一个参数：
//
//	rep, err := zhiduoxingxls.Convert("首层砼柱测试结果.json",
//		zhiduoxingxls.Options{Overwrite: true})
//
// 生成的 .xls 是真正的 OLE2 + BIFF8 二进制工作簿（可被 Excel、WPS、
// LibreOffice、xlrd 直接打开），并且本包不依赖任何第三方库。
//
// JSON 的解析是容错的：允许 BOM、Markdown 代码围栏、// 与 /* */ 注释、
// 单引号字符串、尾随逗号，也会自动从混合文本中抽取 JSON 片段；
// 全部尝试失败时才返回错误，不会因脏数据直接 panic。
package zhiduoxingxls

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nn824/Tobiao/internal/build"
	"github.com/nn824/Tobiao/internal/geom"
	"github.com/nn824/Tobiao/internal/grid"
	"github.com/nn824/Tobiao/internal/model"
	"github.com/nn824/Tobiao/internal/safejson"
	"github.com/nn824/Tobiao/internal/spec"
	"github.com/nn824/Tobiao/internal/xls"
)

// 计算结果分组名，供 Options.Section 使用。
const (
	SectionNorm = "norm" // Norm 分组：量名与计算书模板一一对应（默认）
	SectionList = "list" // List 分组
)

// 哨兵错误，可用 errors.Is 判断。
var (
	ErrEmptyInput   = errors.New("输入 JSON 为空")
	ErrParseJSON    = errors.New("解析 JSON 失败")
	ErrOutputExists = errors.New("输出文件已存在")
)

// Options 是转换参数，零值即为一套可用的默认参数
// （Norm 分组、输出同名 .xls、输出数量行、不覆盖已存在文件）。
type Options struct {
	// Out 是输出 .xls 文件路径；留空时取「与入参 JSON 同名、扩展名换成 .xls」。
	Out string

	// Section 选择计算结果分组：SectionNorm（默认）或 SectionList。
	Section string

	// Floor 是楼层名，如「首层」；留空时按 JSON 中的楼层号推导。
	Floor string

	// SheetName 是工作表名；留空时生成「绘图输入构件工程量计算书-<楼层>」。
	SheetName string

	// Mapping 是可选的规则覆盖文件（JSON）路径，用于修正量名、单位、顺序。
	// 传空字符串表示使用内置规则表。
	Mapping string

	// ModelPath 是智多星「三维模型」导出 JSON 的路径（含图元坐标）。
	//
	// 计算结果 JSON 本身不带坐标，「图元位置」列需要靠这份模型文件里的
	// 坐标 + 轴网换算得到。非空时会自动生成「图元位置」列，
	// 形如 <6,D>、<4-150,A+150>、<10+2400,E-300>。
	ModelPath string

	// AxesPath 是轴网 JSON 文件的路径。**换工程时改这一个入参就够了。**
	//
	// 直接支持智多星导出的原生轴网文件（形如 [{"axis":{"axisLabel":"A"},
	// "distance":0,"direction":"horizontal"}, ...]），也支持简写格式：
	//
	//	{"x":[{"label":"1","pos":0},{"label":"2","pos":9900}],
	//	 "y":[{"label":"A","pos":0},{"label":"B","pos":4500}]}
	//
	// 两种格式会自动识别，不需要预先转换。
	AxesPath string

	// Axes 在代码里直接给出轴网；优先级高于 AxesPath。
	// 两者都不给时用内置轴网（智多星首层：横向 1..11，纵向 A..E）。
	Axes *Axes

	// Positions 是「图元 CID → 图元位置」对照表，用于填充「图元位置」列。
	// 优先级高于 ModelPath；留 nil 时会尝试自动读取 <入参 JSON 同名>.positions.csv。
	Positions map[string]string

	// OmitQuantity 为 true 时不输出「数量」行。
	// JSON 未导出该量，默认按「每个图元计 1」生成，与计算书模板一致。
	OmitQuantity bool

	// AppendExtra 为 true 时，把类型规则之外的工程量追加到明细末尾。
	// 默认关闭，与真实计算书一致。
	AppendExtra bool

	// Overwrite 为 true 时允许覆盖已存在的输出文件。
	// 为 false 且目标已存在时返回 ErrOutputExists，避免误删数据。
	Overwrite bool
}

// Axis 是轴网中的一条轴线。
type Axis struct {
	Label string  `json:"label"` // 轴号，如 "6"、"D"
	Pos   float64 `json:"pos"`   // 坐标：横向为 x；纵向为从起始轴（A）量起的距离
}

// Axes 是轴网定义。X 为横向轴（数字轴号），Y 为纵向轴（字母轴号，按 A→E 排列）。
// 纵向用的是「距 A 轴的距离」，因此 x=0 且 y=0 即 A 轴与 1 轴的交点。
type Axes struct {
	X []Axis `json:"x"`
	Y []Axis `json:"y"`
}

// DefaultAxes 返回内置轴网（智多星首层）的副本。
// 轴距：横向 9900/9000/9000/5400/800/7800/5600/9000/9000/9900，纵向 4500/8700/8400/8400。
func DefaultAxes() *Axes {
	g := grid.Default()
	a := &Axes{}
	for _, x := range g.X {
		a.X = append(a.X, Axis{Label: x.Label, Pos: x.Pos})
	}
	for _, y := range g.Y {
		a.Y = append(a.Y, Axis{Label: y.Label, Pos: y.Pos})
	}
	return a
}

// LoadAxes 从 JSON 文件读取轴网定义，用于换工程时不必重新编译。
//
// 自动识别两种格式。
//
// ① 智多星导出的原生轴网文件（推荐，导出即可用，不用手改）：
//
//	[
//	  {"axis":{"axisLabel":"A"}, "distance":0,     "direction":"horizontal"},
//	  {"axis":{"axisLabel":"B"}, "distance":6600,  "direction":"horizontal"},
//	  {"axis":{"axisLabel":"1"}, "distance":0,     "direction":"vertical"},
//	  {"axis":{"axisLabel":"2"}, "distance":6600,  "direction":"vertical"}
//	]
//
// 其中 direction=horizontal 的是水平轴线（轴号为字母，归到纵向），
// direction=vertical 的是垂直轴线（轴号为数字，归到横向），
// distance 是该轴到起始轴的距离，单位 mm。左右/上下重复出现的同一条轴会自动去重。
//
// ② 简写格式，pos 是绝对坐标（mm），横向取模型 x，纵向取 -z：
//
//	{"x": [{"label":"1","pos":0}, {"label":"2","pos":9900}],
//	 "y": [{"label":"A","pos":0}, {"label":"B","pos":4500}]}
//
// 轴号可以带附加号（如 "1/1"、"2A"）。条目会按 pos 自动升序，录入顺序无所谓。
func LoadAxes(path string) (*Axes, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("zhiduoxingxls: 读取轴网文件 %s 失败: %w", path, err)
	}
	a, err := parseAxes(data)
	if err != nil {
		return nil, fmt.Errorf("zhiduoxingxls: 轴网文件 %s 格式不对: %w", path, err)
	}
	return a, nil
}

// parseAxes 按文件开头是 [ 还是 { 分派到两种轴网格式。
func parseAxes(data []byte) (*Axes, error) {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		return parseNativeAxes(data)
	}
	var a Axes
	if err := safejson.Parse(data, &a); err != nil {
		return nil, err
	}
	return finishAxes(&a)
}

// parseNativeAxes 解析智多星原生轴网数组。
func parseNativeAxes(data []byte) (*Axes, error) {
	var raw []struct {
		Axis struct {
			AxisLabel string `json:"axisLabel"`
		} `json:"axis"`
		Distance  float64 `json:"distance"`
		Direction string  `json:"direction"`
	}
	if err := safejson.Parse(data, &raw); err != nil {
		return nil, err
	}
	a := &Axes{}
	seen := map[string]bool{}
	for _, r := range raw {
		label := strings.TrimSpace(r.Axis.AxisLabel)
		if label == "" {
			return nil, fmt.Errorf("有轴号为空的轴线条目")
		}
		// 同一根轴会在左右（或上下）各出现一次，只取第一次。
		key := strings.ToLower(r.Direction) + "|" + label
		if seen[key] {
			continue
		}
		seen[key] = true
		switch strings.ToLower(r.Direction) {
		case "horizontal":
			a.Y = append(a.Y, Axis{Label: label, Pos: r.Distance})
		case "vertical":
			a.X = append(a.X, Axis{Label: label, Pos: r.Distance})
		default:
			return nil, fmt.Errorf("轴 %s 的 direction 是 %q，只能填 horizontal 或 vertical", label, r.Direction)
		}
	}
	return finishAxes(a)
}

// finishAxes 校验并排序，两种格式共用。
func finishAxes(a *Axes) (*Axes, error) {
	if len(a.X) == 0 && len(a.Y) == 0 {
		return nil, fmt.Errorf("既没有横向轴也没有纵向轴定义")
	}
	for _, list := range [][]Axis{a.X, a.Y} {
		for i, ax := range list {
			if strings.TrimSpace(ax.Label) == "" {
				return nil, fmt.Errorf("第 %d 条轴的轴号为空", i+1)
			}
		}
	}
	sortAxes(a.X)
	sortAxes(a.Y)
	return a, nil
}

// WriteAxesFile 把轴网写成 JSON 文件，方便先按内置轴网导出再手工改。
func WriteAxesFile(path string, a *Axes) error {
	if a == nil {
		a = DefaultAxes()
	}
	var b bytes.Buffer
	b.WriteString("{\n  \"x\": [\n")
	writeAxisList(&b, a.X)
	b.WriteString("  ],\n  \"y\": [\n")
	writeAxisList(&b, a.Y)
	b.WriteString("  ]\n}\n")
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func writeAxisList(b *bytes.Buffer, list []Axis) {
	for i, ax := range list {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(b, "    {\"label\": %q, \"pos\": %g}", ax.Label, ax.Pos)
	}
	if len(list) > 0 {
		b.WriteString("\n")
	}
}

func sortAxes(list []Axis) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Pos != list[j].Pos {
			return list[i].Pos < list[j].Pos
		}
		return list[i].Label < list[j].Label
	})
}

// Report 汇总一次转换的诊断信息，便于调用方记日志或提示用户。
type Report struct {
	Section   string // 实际使用的分组（Norm / List）
	Floor     string // 实际使用的楼层名
	SheetName string // 实际使用的工作表名
	Out       string // 实际写出的 .xls 路径（ConvertBytes 时为空）

	Components int    // 参与转换的图元数
	Groups     int    // 构件分组数（按「构件子类 + 名称」聚合）
	Rows       int    // 生成的总行数
	Positioned int    // 成功填上「图元位置」的图元数
	Axes       string // 本次实际使用的轴网来源说明（自动推断时会写明，便于核对）

	Skipped       map[string]int // 跳过原因 → 图元数
	UnknownZL     map[string]int // 未收录的构件子类 → 图元数
	UnmappedCodes map[string]int // 未收录的工程量编码 → 出现次数
	HiddenCodes   map[string]int // 属于类型规则之外、未输出的编码 → 出现次数
	SectionErrors []string       // JSON 分组内部的错误条目
	NullRules     int            // 计算规则返回空值的条目数
	Warnings      []string       // 提示信息（去重、上限 60 条）
}

// Convert 读取 jsonPath 指向的测试结果 JSON，转换为 .xls 并写出到磁盘。
//
// jsonPath 是唯一必填的入参；输出路径由 opts.Out 指定，留空时与入参同名。
// 返回的 *Report 可用于日志或界面提示；失败时返回的 Report 为 nil。
func Convert(jsonPath string, opts Options) (*Report, error) {
	if strings.TrimSpace(jsonPath) == "" {
		return nil, fmt.Errorf("zhiduoxingxls: 缺少输入 JSON 文件路径")
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("zhiduoxingxls: 读取输入文件 %s 失败: %w", jsonPath, err)
	}

	out := strings.TrimSpace(opts.Out)
	if out == "" {
		out = strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".xls"
	}
	if !opts.Overwrite {
		if _, err := os.Stat(out); err == nil {
			return nil, fmt.Errorf("%w：%s（把 Options.Overwrite 置为 true 可覆盖）", ErrOutputExists, out)
		}
	}

	stream, rep, err := convert(data, opts, jsonPath)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(out, stream); err != nil {
		return nil, err
	}
	rep.Out = out
	return rep, nil
}

// ConvertBytes 直接处理 JSON 字节内容并返回 .xls 字节流，不落盘。
// 适合输入来自网络请求或内存的场景，此时 opts.Out 被忽略。
func ConvertBytes(data []byte, opts Options) ([]byte, *Report, error) {
	return convert(data, opts, "")
}

// convert 是 Convert 与 ConvertBytes 的共用实现。
// srcPath 为入参文件路径（用于推导 positions.csv），来自内存时传空字符串。
func convert(data []byte, opts Options, srcPath string) ([]byte, *Report, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil, fmt.Errorf("zhiduoxingxls: %w", ErrEmptyInput)
	}

	// 1) 容错解析 JSON
	var doc model.Document
	if err := safejson.Parse(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("zhiduoxingxls: %w: %v", ErrParseJSON, err)
	}

	// 2) 规则表（可选外部覆盖）
	table := spec.NewTable()
	if opts.Mapping != "" {
		if err := spec.LoadOverrides(table, opts.Mapping); err != nil {
			return nil, nil, err
		}
	}

	// 2.5) 轴网：Options.Axes > AxesPath（外部 JSON）> 内置。
	var g *grid.Grid // nil 时 Resolver 内部退回内置轴网
	axesDesc := builtinAxesDesc
	switch {
	case opts.Axes != nil:
		g = toGrid(opts.Axes)
		axesDesc = "调用方指定（Options.Axes）"
	case strings.TrimSpace(opts.AxesPath) != "":
		a, err := LoadAxes(opts.AxesPath)
		if err != nil {
			return nil, nil, err
		}
		g = toGrid(a)
		axesDesc = fmt.Sprintf("%s（横向 %d 轴、纵向 %d 轴）", opts.AxesPath, len(a.X), len(a.Y))
	}

	// 3) 图元位置：
	//    ① 显式传入 Positions 优先；
	//    ② 其次用模型文件（含坐标）+ 轴网自动换算；
	//    ③ 最后尝试读同目录下的 positions.csv。
	positions := opts.Positions
	var positionFor func(cid, pid string) string
	if positions == nil && strings.TrimSpace(opts.ModelPath) != "" {
		gm, err := geom.Load(opts.ModelPath)
		if err != nil {
			return nil, nil, err
		}
		positionFor = gm.Resolver(g).Position
	}
	if positions == nil && positionFor == nil && srcPath != "" {
		if p := strings.TrimSuffix(srcPath, filepath.Ext(srcPath)) + ".positions.csv"; fileExists(p) {
			m, err := build.LoadPositions(p)
			if err != nil {
				return nil, nil, err
			}
			positions = m
		}
	}

	section := opts.Section
	if strings.TrimSpace(section) == "" {
		section = SectionNorm
	}

	// 4) 装配表格
	wb, brep, err := build.Convert(&doc, table, build.Options{
		Section:      section,
		FloorName:    opts.Floor,
		SheetName:    opts.SheetName,
		EmitQuantity: !opts.OmitQuantity,
		AppendExtra:  opts.AppendExtra,
		Positions:    positions,
		PositionFor:  positionFor,
	})
	if err != nil {
		return nil, nil, err
	}
	switch {
	case brep.Positioned == 0 && positionFor != nil:
		brep.Warnings = append(brep.Warnings, "模型文件里没有能与计算结果对上的图元坐标，「图元位置」列仍为空；"+
			"请确认模型 JSON 与计算结果 JSON 来自同一次导出")
	case brep.Positioned == 0:
		brep.Warnings = append(brep.Warnings, "「图元位置」列留空：计算结果 JSON 不含坐标。"+
			"设置 Options.ModelPath 指向智多星导出的三维模型 JSON 即可自动填充；"+
			"也可设置 Options.Positions，或在输入文件同目录放置 <同名>.positions.csv（两列：CID,位置）")
	}

	// 5) 生成 BIFF8 工作簿流，再封装成 OLE2 复合文档（.xls 的真实容器）。
	//    两步都不能省：只写出 BIFF 流会得到一个 Excel 无法识别的文件。
	book, err := wb.Write()
	if err != nil {
		return nil, nil, fmt.Errorf("zhiduoxingxls: 生成工作簿失败: %w", err)
	}
	var buf bytes.Buffer
	buf.Grow(len(book) + 512)
	if err := xls.WriteCFB(&buf, "Workbook", book); err != nil {
		return nil, nil, fmt.Errorf("zhiduoxingxls: 写出 OLE2 容器失败: %w", err)
	}

	rep := toReport(brep)
	rep.Rows = len(wb.Sheet.Rows)
	// 没有走坐标换算时（既无模型也无对照表），轴网来源没有意义，留空。
	if positionFor == nil && positions == nil {
		axesDesc = ""
	}
	rep.Axes = axesDesc
	return buf.Bytes(), rep, nil
}

// builtinAxesDesc 是内置轴网的来源说明，写进 Report.Axes。
const builtinAxesDesc = "内置（智多星首层：横向 1..11，纵向 A..E）"

// toGrid 把公开的轴网定义转换为内部结构；a 为 nil 时返回内置轴网。
func toGrid(a *Axes) *grid.Grid {
	if a == nil {
		return grid.Default()
	}
	g := &grid.Grid{}
	for _, x := range a.X {
		g.X = append(g.X, grid.Axis{Label: x.Label, Pos: x.Pos})
	}
	for _, y := range a.Y {
		g.Y = append(g.Y, grid.Axis{Label: y.Label, Pos: y.Pos})
	}
	if len(g.X) == 0 && len(g.Y) == 0 {
		return grid.Default()
	}
	return g
}

// toReport 把内部诊断结构转换为稳定的公开结构。
func toReport(b *build.Report) *Report {
	return &Report{
		Section:       b.Section,
		Floor:         b.Floor,
		SheetName:     b.SheetName,
		Components:    b.Components,
		Groups:        b.Groups,
		Positioned:    b.Positioned,
		Skipped:       copyCounts(b.Skipped),
		UnknownZL:     copyCounts(b.UnknownZL),
		UnmappedCodes: copyCounts(b.UnmappedCodes),
		HiddenCodes:   copyCounts(b.HiddenCodes),
		SectionErrors: append([]string(nil), b.SectionErrors...),
		NullRules:     b.NullRules,
		Warnings:      append([]string(nil), b.Warnings...),
	}
}

func copyCounts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// writeFileAtomic 先写同目录临时文件再改名，避免写出半截文件。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".zhiduoxing-*.tmp")
	if err != nil {
		return fmt.Errorf("zhiduoxingxls: 创建临时文件失败: %w", err)
	}
	tmp := f.Name()
	defer func() {
		if _, statErr := os.Stat(tmp); statErr == nil {
			os.Remove(tmp)
		}
	}()

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("zhiduoxingxls: 写出临时文件失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("zhiduoxingxls: 落盘失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("zhiduoxingxls: 关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("zhiduoxingxls: 重命名到 %s 失败: %w", path, err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
