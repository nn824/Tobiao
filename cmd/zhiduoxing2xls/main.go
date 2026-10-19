// Command zhiduoxing2xls 把智多星「工程量计算结果」JSON 文件转换成
// 与新.xls 相同版式的「绘图输入构件工程量计算书」.xls 文件。
//
// 用法示例：
//
//	zhiduoxing2xls 首层砼柱测试结果.json
//	zhiduoxing2xls -in 结果.json -out 计算书.xls
//	zhiduoxing2xls -in 结果.json -section list -floor 首层 -emit-qty=false
//
// 运行 zhiduoxing2xls -h 查看全部参数。
//
// 本命令只是包 zhiduoxingxls 的薄封装；在 Go 项目里应直接 import 该包调用，
// 而不是通过命令行。
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	zhiduoxingxls "github.com/ninglang/zhiduoxing-xls"
)

type options struct {
	in        string
	out       string
	model     string
	axes      string
	dumpAxes  string
	section   string
	floor     string
	sheet     string
	mapping   string
	extras    bool
	emitQty   bool
	quiet     bool
	overwrite bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		os.Exit(1)
	}
}

func run() error {
	// 子命令 compare：比对两份计算书的工程量计算式，不属于转换流程。
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "compare", "cmp":
			return runCompare(os.Args[2:])
		}
	}

	var o options
	flag.StringVar(&o.in, "in", "", "输入 JSON 文件路径（也可直接作为位置参数传入）")
	flag.StringVar(&o.out, "out", "", "输出 .xls 文件路径（默认与输入同名）")
	flag.StringVar(&o.model, "model", "", "智多星导出的三维模型 JSON（含图元坐标）；给出后自动填充「图元位置」列")
	flag.StringVar(&o.axes, "axes", "", "轴网 JSON 文件；**换工程时只改这个**。支持智多星原生轴网格式与 {x:[],y:[]} 简写格式")
	flag.StringVar(&o.dumpAxes, "dump-axes", "", "把内置轴网导出成 JSON 模板后退出，供换工程时照图纸改")
	flag.StringVar(&o.section, "section", zhiduoxingxls.SectionNorm, "使用哪个计算结果分组：norm（默认）或 list")
	flag.StringVar(&o.floor, "floor", "", "楼层名，如「首层」；留空则按楼层号推导")
	flag.StringVar(&o.sheet, "sheet", "", "工作表名；留空则按「绘图输入构件工程量计算书-楼层」生成")
	flag.StringVar(&o.mapping, "mapping", "", "可选的规则覆盖文件（JSON），用于修正量名/单位/顺序")
	flag.BoolVar(&o.extras, "append-extra", false, "把类型规则之外的工程量追加到明细末尾（默认关闭，与真实文件一致）")
	flag.BoolVar(&o.emitQty, "emit-qty", true, "是否输出「数量」行（JSON 未导出该量，按图元恒为 1 生成）")
	flag.BoolVar(&o.quiet, "quiet", false, "只输出结果文件，不打印统计信息")
	flag.BoolVar(&o.overwrite, "y", false, "允许覆盖已存在的输出文件")
	flag.Usage = usage
	flag.Parse()

	// 支持把 JSON 文件作为位置参数传入；由于 flag 在遇到第一个非选项参数时
	// 会停止解析，这里把位置参数之后的参数再解析一遍，使
	// `zhiduoxing2xls 结果.json -y` 也能正确生效。
	rest := flag.Args()
	if o.in == "" && len(rest) > 0 {
		o.in = rest[0]
		if len(rest) > 1 {
			if err := flag.CommandLine.Parse(rest[1:]); err != nil {
				return err
			}
		}
	}
	// -dump-axes 是独立动作，不需要输入文件。
	if o.dumpAxes != "" {
		if err := zhiduoxingxls.WriteAxesFile(o.dumpAxes, nil); err != nil {
			return err
		}
		if !o.quiet {
			fmt.Fprintf(os.Stderr, "\n已导出轴网模板: %s（照图纸改完用 -axes 传回）\n", o.dumpAxes)
		}
		return nil
	}
	if o.in == "" {
		flag.Usage()
		return fmt.Errorf("缺少输入 JSON 文件路径")
	}

	rep, err := zhiduoxingxls.Convert(o.in, zhiduoxingxls.Options{
		Out:          o.out,
		ModelPath:    o.model,
		AxesPath:     o.axes,
		Section:      o.section,
		Floor:        o.floor,
		SheetName:    o.sheet,
		Mapping:      o.mapping,
		OmitQuantity: !o.emitQty,
		AppendExtra:  o.extras,
		Overwrite:    o.overwrite,
	})
	if err != nil {
		return err
	}
	if !o.quiet {
		printReport(rep, o)
	}
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `zhiduoxing2xls —— 智多星计算结果 JSON → 绘图输入构件工程量计算书 .xls

用法：
  zhiduoxing2xls [参数] <结果.json>
  zhiduoxing2xls compare [参数]       比对两份计算书的工程量计算式

参数：
`)
	flag.PrintDefaults()
	fmt.Fprint(os.Stderr, `
示例：
  zhiduoxing2xls 首层砼柱测试结果.json
  zhiduoxing2xls -in 结果.json -out 计算书.xls -y
  zhiduoxing2xls -in 结果.json -model 模型.json -axes 轴网.json -y

图元位置：
  计算结果 JSON 只有算量、没有坐标，所以「图元位置」列默认留空。
  用 -model 指向智多星导出的三维模型 JSON（含图元坐标）即可自动填上，
  形如 <6,D>、<4-150,A+150>、<10+2400,E-300>。

轴网（换工程时只改这一个入参）：
  -axes 直接吃智多星导出的原生轴网文件，不用转换格式：
    [{"axis":{"axisLabel":"A"},"distance":0,"direction":"horizontal"}, ...]
  也吃简写格式（pos 为绝对坐标 mm，横向取模型 x，纵向取 -z）：
    {"x": [{"label":"1","pos":0}, {"label":"2","pos":9900}],
     "y": [{"label":"A","pos":0}, {"label":"B","pos":4500}]}
  条目顺序无所谓，会按 pos 自动排序。
  不带 -axes 时用内置轴网（智多星首层 1..11 / A..E）；可先导出模板再改：
    zhiduoxing2xls -dump-axes 轴网.json
`)
}

// runCompare 执行 compare 子命令：比对两份计算书的工程量计算式。
//
// 用法：
//
//	zhiduoxing2xls compare -gen 计算书.xls -ref 新.xls -json 测试结果.json
//	zhiduoxing2xls compare 计算书.xls 新.xls 测试结果.json
func runCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	var (
		gen       string
		ref       string
		jsonPath  string
		out       string
		floor     string
		section   string
		overwrite bool
		quiet     bool
	)
	fs.StringVar(&gen, "gen", "", "已输出的计算书 .xls（必填，也可作第一个位置参数）")
	fs.StringVar(&ref, "ref", "", "参照的计算书 .xls，如 新.xls（必填，也可作第二个位置参数）")
	fs.StringVar(&jsonPath, "json", "", "测试结果 JSON，用来确定楼层名（必填，也可作第三个位置参数）")
	fs.StringVar(&out, "out", "", "比对结果 .xls；留空取 Generated 同目录的「<名字>-比对.xls」")
	fs.StringVar(&floor, "floor", "", "直接指定楼层名，优先于 -json")
	fs.StringVar(&section, "section", zhiduoxingxls.SectionNorm, "从 JSON 的哪个分组取楼层：norm（默认）或 list")
	fs.BoolVar(&overwrite, "y", false, "允许覆盖已存在的输出文件")
	fs.BoolVar(&quiet, "quiet", false, "只输出结果文件，不打印统计")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `zhiduoxing2xls compare —— 按坐标比对两份计算书的工程量计算式

用法：
  zhiduoxing2xls compare [参数] <计算书.xls> <参照.xls> <测试结果.json>

比对键：楼层 + 构件名 + 图元位置（坐标）。
同一坐标、同一构件下的「工程量计算式」会各自收成一个集合，
集合相同记「一致」，不同记「不一致」，只在一侧出现分别记「仅计算书有」「仅参考文件有」。

参数：
`)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, `
示例：
  zhiduoxing2xls compare 新工程计算书.xls 新.xls 测试结果.json
  zhiduoxing2xls compare -gen 计算书.xls -ref 新.xls -json 结果.json -out 比对.xls -y

楼层名：
  比对表的「楼层」列一律按测试结果 JSON 推导（首层 → 首层，2 → 二层），
  不用计算书里写的楼层名；也可以用 -floor 直接指定。
`)
	}
	// flag 遇到第一个非选项参数就会停止解析，而这里的位置参数（3 个文件）
	// 可能与选项混排，所以要反复「解析 → 取走一个位置参数 → 继续解析」。
	var positional []string
	remaining := args
	for len(remaining) > 0 {
		if err := fs.Parse(remaining); err != nil {
			if err == flag.ErrHelp {
				return nil
			}
			return err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		remaining = rest[1:]
	}

	if gen == "" && len(positional) > 0 {
		gen, positional = positional[0], positional[1:]
	}
	if ref == "" && len(positional) > 0 {
		ref, positional = positional[0], positional[1:]
	}
	if jsonPath == "" && len(positional) > 0 {
		jsonPath = positional[0]
	}

	rep, err := zhiduoxingxls.Compare(zhiduoxingxls.CompareOptions{
		Generated:  gen,
		Reference:  ref,
		ResultJSON: jsonPath,
		Out:        out,
		Floor:      floor,
		Section:    section,
		Overwrite:  overwrite,
	})
	if err != nil {
		return err
	}
	if quiet {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n计算书    : %s（图元 %d 行）\n", gen, rep.GeneratedRows)
	fmt.Fprintf(&b, "参照文件  : %s（图元 %d 行）\n", ref, rep.ReferenceRows)
	fmt.Fprintf(&b, "楼层      : %s\n", rep.Floor)
	fmt.Fprintf(&b, "比对键    : %d\n", rep.Items)
	fmt.Fprintf(&b, "  一致    : %d\n", rep.Same)
	fmt.Fprintf(&b, "  不一致  : %d\n", rep.Diff)
	fmt.Fprintf(&b, "  仅计算书: %d\n", rep.OnlyGenerated)
	fmt.Fprintf(&b, "  仅参考  : %d\n", rep.OnlyReference)
	fmt.Fprintf(&b, "输出文件  : %s\n", rep.Out)
	for _, w := range rep.Warnings {
		fmt.Fprintf(&b, "提示：%s\n", w)
	}
	fmt.Fprint(os.Stderr, b.String())
	return nil
}

func printReport(rep *zhiduoxingxls.Report, o options) {
	var b strings.Builder
	fmt.Fprintf(&b, "\n源文件    : %s\n", o.in)
	fmt.Fprintf(&b, "输出文件  : %s\n", rep.Out)
	fmt.Fprintf(&b, "计算结果组: %s\n", rep.Section)
	fmt.Fprintf(&b, "工作表    : %s\n", rep.SheetName)
	fmt.Fprintf(&b, "图元数    : %d，构件分组数：%d，总行数：%d\n", rep.Components, rep.Groups, rep.Rows)
	if rep.Positioned > 0 {
		fmt.Fprintf(&b, "图元位置  : 已填充 %d 个图元", rep.Positioned)
		if o.model != "" {
			fmt.Fprintf(&b, "（来源：%s）", o.model)
		}
		fmt.Fprintln(&b)
		if rep.Axes != "" {
			fmt.Fprintf(&b, "轴网      : %s\n", rep.Axes)
		}
	}
	if rep.NullRules > 0 {
		fmt.Fprintf(&b, "计算规则返回空值的条目：%d\n", rep.NullRules)
	}
	if len(rep.Skipped) > 0 {
		fmt.Fprintf(&b, "已跳过    : %s\n", countString(rep.Skipped))
	}
	if len(rep.UnknownZL) > 0 {
		fmt.Fprintf(&b, "未收录类型: %s\n", countString(rep.UnknownZL))
	}
	if len(rep.HiddenCodes) > 0 {
		fmt.Fprintf(&b, "规则外工程量（未输出，可用 -append-extra 打开）: %s\n", countString(rep.HiddenCodes))
	}
	if len(rep.UnmappedCodes) > 0 {
		fmt.Fprintf(&b, "未收录编码: %s\n", countString(rep.UnmappedCodes))
	}
	if len(rep.SectionErrors) > 0 {
		fmt.Fprintf(&b, "分组内错误条目（%d 条）：\n", len(rep.SectionErrors))
		for _, e := range rep.SectionErrors {
			fmt.Fprintf(&b, "  - %s\n", e)
		}
	}
	if len(rep.Warnings) > 0 {
		fmt.Fprintf(&b, "提示：\n")
		for _, w := range rep.Warnings {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}
	fmt.Fprint(os.Stderr, b.String())
}

func countString(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, m[k]))
	}
	return strings.Join(parts, "、")
}
