package zhiduoxingxls

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cmp "github.com/nn824/Tobiao/internal/compare"
	"github.com/nn824/Tobiao/internal/model"
	"github.com/nn824/Tobiao/internal/safejson"
	"github.com/nn824/Tobiao/internal/spec"
	"github.com/nn824/Tobiao/internal/xls"
)

// CompareOptions 是「计算书比对」的参数。
//
// 三个入参都必须给：
//
//	rep, err := zhiduoxingxls.Compare(zhiduoxingxls.CompareOptions{
//		Generated:  "新工程计算书.xls",   // 本工具刚输出的计算书
//		Reference:  "新.xls",            // 参照的计算书
//		ResultJSON: "测试结果.json",      // 只用它来确定楼层名
//		Overwrite:  true,
//	})
type CompareOptions struct {
	// Generated 是已输出的计算书 .xls（必填）。
	Generated string

	// Reference 是参照的计算书 .xls，例如「新.xls」（必填）。
	Reference string

	// ResultJSON 是智多星测试结果 JSON，只用来取楼层名（必填）。
	// 输出表格的「楼层」列一律用这里推导出的名字。
	ResultJSON string

	// Out 是比对结果 .xls 路径；留空时取 Generated 同目录下的「<名字>-比对.xls」。
	Out string

	// Floor 直接指定楼层名，优先于 ResultJSON。
	Floor string

	// Section 决定从 ResultJSON 的哪个分组取楼层：norm（默认）或 list。
	Section string

	// Overwrite 为 true 时允许覆盖已存在的输出文件。
	Overwrite bool
}

// CompareReport 汇总一次比对的结果，便于打印或记日志。
type CompareReport struct {
	Out   string // 实际写出的 .xls
	Floor string // 实际使用的楼层名

	GeneratedRows int // 计算书里解析出的图元行数
	ReferenceRows int // 参照文件里解析出的图元行数

	Items         int // 比对键总数
	Same          int // 计算式一致
	Diff          int // 同一坐标两边算式不同
	OnlyGenerated int // 只在计算书里出现的坐标
	OnlyReference int // 只在参照文件里出现的坐标

	Warnings []string
}

// Compare 把两份计算书按「楼层 + 构件名 + 图元位置」逐条比对，
// 收集两侧的「工程量计算式」集合并写出比对结果 .xls。
func Compare(opt CompareOptions) (*CompareReport, error) {
	if strings.TrimSpace(opt.Generated) == "" {
		return nil, errors.New("zhiduoxingxls: 缺少计算书 .xls 路径（CompareOptions.Generated）")
	}
	if strings.TrimSpace(opt.Reference) == "" {
		return nil, errors.New("zhiduoxingxls: 缺少参照计算书 .xls 路径（CompareOptions.Reference）")
	}

	out := strings.TrimSpace(opt.Out)
	if out == "" {
		base := strings.TrimSuffix(filepath.Base(opt.Generated), filepath.Ext(opt.Generated))
		out = filepath.Join(filepath.Dir(opt.Generated), base+"-比对.xls")
	}
	if !opt.Overwrite {
		if _, err := os.Stat(out); err == nil {
			return nil, fmt.Errorf("%w：%s（把 CompareOptions.Overwrite 置为 true 可覆盖）", ErrOutputExists, out)
		}
	}

	floor, warns, err := resolveCompareFloor(opt)
	if err != nil {
		return nil, err
	}

	genBook, err := xls.ReadFile(opt.Generated)
	if err != nil {
		return nil, fmt.Errorf("zhiduoxingxls: 读取计算书失败: %w", err)
	}
	refBook, err := xls.ReadFile(opt.Reference)
	if err != nil {
		return nil, fmt.Errorf("zhiduoxingxls: 读取参照文件失败: %w", err)
	}
	if len(genBook.Sheets) == 0 {
		return nil, fmt.Errorf("zhiduoxingxls: %s 里没有工作表", opt.Generated)
	}
	if len(refBook.Sheets) == 0 {
		return nil, fmt.Errorf("zhiduoxingxls: %s 里没有工作表", opt.Reference)
	}

	genRows := cmp.Parse(genBook.Sheets[0], floor)
	refRows := cmp.Parse(refBook.Sheets[0], floor)
	if len(genRows) == 0 {
		warns = append(warns, "计算书里没有解析到图元行；请确认它是「绘图输入构件工程量计算书」版式")
	}
	if len(refRows) == 0 {
		warns = append(warns, "参照文件里没有解析到图元行；请确认它也是同一种计算书版式")
	}

	items := cmp.Collect(genRows, refRows)
	data, err := cmp.Build(items, cmp.WriteOptions{})
	if err != nil {
		return nil, fmt.Errorf("zhiduoxingxls: 生成比对结果失败: %w", err)
	}
	if err := writeFileAtomic(out, data); err != nil {
		return nil, err
	}

	st := cmp.Count(items)
	return &CompareReport{
		Out:           out,
		Floor:         floor,
		GeneratedRows: len(genRows),
		ReferenceRows: len(refRows),
		Items:         st.Items,
		Same:          st.Same,
		Diff:          st.Diff,
		OnlyGenerated: st.OnlyGenerated,
		OnlyReference: st.OnlyReference,
		Warnings:      warns,
	}, nil
}

// resolveCompareFloor 决定输出用的楼层名：显式指定 > 测试结果 JSON > 原样保留。
func resolveCompareFloor(opt CompareOptions) (string, []string, error) {
	if f := strings.TrimSpace(opt.Floor); f != "" {
		return f, nil, nil
	}
	if strings.TrimSpace(opt.ResultJSON) == "" {
		return "", nil, nil
	}
	data, err := os.ReadFile(opt.ResultJSON)
	if err != nil {
		return "", nil, fmt.Errorf("zhiduoxingxls: 读取测试结果 JSON 失败: %w", err)
	}
	var doc model.Document
	if err := safejson.Parse(data, &doc); err != nil {
		return "", nil, fmt.Errorf("zhiduoxingxls: %w: %v", ErrParseJSON, err)
	}
	sec := spec.SectionOf(&doc, opt.Section)
	fl := sec.FirstFloor()
	if fl == nil {
		return "", []string{"测试结果 JSON 里没有楼层信息，楼层名沿用计算书里的值"}, nil
	}
	var warns []string
	if n := countFloors(sec); n > 1 {
		warns = append(warns, fmt.Sprintf("测试结果 JSON 里有 %d 个楼层，比对表统一使用第一个楼层名「%s」",
			n, spec.FloorName(int(fl.LCH))))
	}
	return spec.FloorName(int(fl.LCH)), warns, nil
}

// countFloors 统计分组里的楼层数。
func countFloors(sec *model.Section) int {
	n := 0
	for _, sp := range sec.Project.SubProject {
		n += len(sp.Floor)
	}
	return n
}
