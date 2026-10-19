// Package spec 保存「智多星工程量计算书」的渲染规则表：
// 构件子类（ZL）到构件类型中文名的映射、工程量编码到显示名/原始量标签/单位/顺序的映射。
//
// 表中的取值全部来自对真实导出文件 新.xls 的逐条比对；
// 少数无法从样本中直接验证的项已用 [推断] 标注，可通过外部 JSON 覆盖文件修正。
package spec

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ninglang/zhiduoxing-xls/internal/model"
)

// Unit 是工程量的量纲，决定 JSL（整数）还原为实数时的除数。
type Unit int

// 量纲常量。JSL 以毫米为长度基准：长度 = mm，面积 = mm²，体积 = mm³。
const (
	UnitLength Unit = iota
	UnitArea
	UnitVolume
	UnitCount
)

// unitFromSuffix 从 JSSJG 的单位后缀反推量纲（兜底用）。
func unitFromSuffix(s string) (Unit, string, bool) {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "=")
	switch {
	case strings.HasSuffix(t, "m³"):
		return UnitVolume, "m3", true
	case strings.HasSuffix(t, "m²"):
		return UnitArea, "m2", true
	case strings.HasSuffix(t, "m3"):
		return UnitVolume, "m3", true
	case strings.HasSuffix(t, "m2"):
		return UnitArea, "m2", true
	case strings.HasSuffix(t, "㎡"):
		return UnitArea, "m2", true
	case strings.HasSuffix(t, "m"):
		return UnitLength, "m", true
	case strings.HasSuffix(t, "mm"):
		return UnitLength, "m", true
	case strings.HasSuffix(t, "根"):
		return UnitCount, "根", true
	case strings.HasSuffix(t, "樘"):
		return UnitCount, "樘", true
	case strings.HasSuffix(t, "个"):
		return UnitCount, "个", true
	case strings.HasSuffix(t, "块"):
		return UnitCount, "块", true
	case strings.HasSuffix(t, "t"), strings.HasSuffix(t, "kg"):
		return UnitCount, t, true
	}
	return UnitLength, "", false
}

// Qty 描述一个工程量在计算书中的呈现方式与位置。
type Qty struct {
	Code   string // 工程量编码，或伪编码 "#SL"（数量）
	Name   string // 显示名
	Raw    string // JSS 为空时使用的「原始量」标签；留空则复用 Name
	Unit   Unit   // 量纲
	Suffix string // 结果单位文本，如 m / m2 / m3 / 根
}

// TypeSpec 描述一个构件类型在计算书中的呈现方式。
type TypeSpec struct {
	ZL      string // 构件子类编码
	Name    string // 构件类型中文名
	Order   int    // 类型排序权重，越小越靠前
	QtyUnit string // 数量行的单位（根/樘/个/块）；留空则不输出数量行
	Items   []Qty  // 按输出顺序排列的工程量
}

// 数量行的伪编码。
const CodeQuantity = "#SL"

func len1(code, name, raw string) Qty {
	return Qty{Code: code, Name: name, Raw: raw, Unit: UnitLength, Suffix: "m"}
}
func area(code, name, raw string) Qty {
	return Qty{Code: code, Name: name, Raw: raw, Unit: UnitArea, Suffix: "m2"}
}
func vol(code, name, raw string) Qty {
	return Qty{Code: code, Name: name, Raw: raw, Unit: UnitVolume, Suffix: "m3"}
}

// table 为内置映射表，顺序即输出顺序。
var table = []TypeSpec{
	{
		ZL: "GJ_Z_TZ", Name: "柱", Order: 10, QtyUnit: "根",
		Items: []Qty{
			len1("GCL_ZC", "周长", "周长"),
			vol("GCL_TJ", "体积", "原始体积"),
			area("GCL_MBMJ", "模板面积", "原始模板面积"),
			area("GCL_CGMBMJ", "超高模板面积", "原始超高模板面积"),
			{Code: CodeQuantity, Name: "数量", Unit: UnitCount, Suffix: "根"},
			len1("GCL_GD", "高度", "原始高度"),
			area("GCL_JMMJ", "截面面积", "原始截面面积"),
		},
	},
	{
		ZL: "GJ_Z_GZZ", Name: "构造柱", Order: 20, QtyUnit: "根",
		Items: []Qty{
			len1("GCL_ZC", "周长", "周长"),
			vol("GCL_TJ", "体积", "原始体积"),
			area("GCL_MBMJ", "模板面积", "原始模板面积"),
			area("GCL_CGMBMJ", "超高模板面积", "原始超高模板面积"),
			{Code: CodeQuantity, Name: "数量", Unit: UnitCount, Suffix: "根"},
			area("GCL_JMMJ", "截面面积", "原始截面面积"),
			len1("GCL_GD", "高度", "原始高度"),
		},
	},
	{
		ZL: "GJ_Q_QTQ", Name: "砌体墙", Order: 30,
		Items: []Qty{
			area("GCL_MJ", "面积", "原始面积"),
			vol("GCL_TJ", "体积", "原始体积"),
			len1("GCL_NBQLGSWPCD", "内部墙梁钢丝网片长度", "内部墙梁钢丝网片原始长度"),
			len1("GCL_NBQZGSWPCD", "内部墙柱钢丝网片长度", "内部墙柱钢丝网片原始长度"),
			len1("GCL_CD", "长度", "长度"),
			len1("GCL_GD", "墙高", "墙高"),
			len1("GCL_HD", "墙厚", "墙厚"),
			area("GCL_NQJSJMJ", "内墙脚手架面积", ""),
		},
	},
	{
		ZL: "GJ_Q_HNTQ", Name: "混凝土墙", Order: 35,
		Items: []Qty{
			area("GCL_MJ", "面积", "原始面积"),
			vol("GCL_TJ", "体积", "原始体积"),
			area("GCL_MBMJ", "模板面积", "原始模板面积"),
			area("GCL_CGMBMJ", "超高模板面积", "原始超高模板面积"),
			len1("GCL_CD", "长度", "长度"),
			len1("GCL_GD", "墙高", "墙高"),
			len1("GCL_HD", "墙厚", "墙厚"),
		},
	},
	{
		ZL: "GJ_MCD_M", Name: "门", Order: 40, QtyUnit: "樘",
		Items: []Qty{
			area("GCL_DKMJ", "洞口面积", "原始洞口面积"),
			area("GCL_KWWMJ", "框外围面积", "原始框外围面积"),
			area("GCL_WJJXDKMJ", "外接矩形洞口面积", "原始外接矩形洞口面积"),
			{Code: CodeQuantity, Name: "数量", Unit: UnitCount, Suffix: "樘"},
			len1("GCL_DKSMCD", "洞口三面长度", "原始洞口三面长度"),
			len1("GCL_DKKD", "洞口宽度", "洞口宽度"),
			len1("GCL_DKGD", "洞口高度", "洞口高度"),
			len1("GCL_DKZC", "洞口周长", "原始洞口周长"),
		},
	},
	{
		ZL: "GJ_MCD_C", Name: "窗", Order: 50, QtyUnit: "樘",
		Items: []Qty{
			area("GCL_DKMJ", "洞口面积", "原始洞口面积"),
			area("GCL_KWWMJ", "框外围面积", "原始框外围面积"),
			{Code: CodeQuantity, Name: "数量", Unit: UnitCount, Suffix: "樘"},
			len1("GCL_DKSMCD", "洞口三面长度", "原始洞口三面长度"),
			len1("GCL_DKKD", "洞口宽度", "洞口宽度"),
			len1("GCL_DKGD", "洞口高度", "洞口高度"),
			len1("GCL_DKZC", "洞口周长", "原始洞口周长"),
		},
	},
	{
		ZL: "GJ_MCD_SD", Name: "墙洞", Order: 60, QtyUnit: "个",
		Items: []Qty{
			area("GCL_DKMJ", "洞口面积", "原始洞口面积"),
			{Code: CodeQuantity, Name: "数量", Unit: UnitCount, Suffix: "个"},
			len1("GCL_DKSMCD", "洞口三面长度", "原始洞口三面长度"),
			len1("GCL_DKKD", "洞口宽度", "洞口宽度"),
			len1("GCL_DKGD", "洞口高度", "洞口高度"),
			len1("GCL_DKZC", "洞口周长", "原始洞口周长"),
		},
	},
	{
		ZL: "GJ_MCD_GL", Name: "过梁", Order: 70, QtyUnit: "个",
		Items: []Qty{
			vol("GCL_TJ", "体积", "原始体积"),
			area("GCL_MBMJ", "模板面积", "原始模板面积"),
			{Code: CodeQuantity, Name: "数量", Unit: UnitCount, Suffix: "个"},
			len1("GCL_CD", "长度", "原始长度"),
			len1("GCL_KD", "宽度", "原始宽度"),
			len1("GCL_GD", "高度", "原始高度"),
		},
	},
	{
		ZL: "GJ_L_L", Name: "梁", Order: 80,
		Items: []Qty{
			vol("GCL_TJ", "体积", "原始体积"),
			area("GCL_MBMJ", "模板面积", "原始模板面积"),
			area("GCL_CGMBMJ", "超高模板面积", "原始超高模板面积"),
			len1("GCL_JMZC", "截面周长", "原始截面周长"),
			len1("GCL_LJC", "梁净长", "梁原始长度"),
			len1("GCL_ZXCD", "轴线长度", "轴线长度"),
			area("GCL_LCMMJ", "梁侧面面积", "原始侧面面积"),
			area("GCL_JMMJ", "截面面积", "原始截面面积"),
			len1("GCL_JMGD", "截面高度", "梁截面高度"),
			len1("GCL_JMKD", "截面宽度", "梁截面宽度"),
		},
	},
	{
		ZL: "GJ_L_QL", Name: "圈梁", Order: 90,
		Items: []Qty{
			vol("GCL_TJ", "体积", "原始体积"),
			area("GCL_MBMJ", "模板面积", "原始模板面积"),
			len1("GCL_JMZC", "截面周长", "原始截面周长"),
			len1("GCL_LJC", "梁净长", "梁原始长度"),
			len1("GCL_ZXCD", "轴线长度", "原始轴线长度"),
			area("GCL_JMMJ", "截面面积", "原始截面面积"),
		},
	},
	{
		ZL: "GJ_B_XJB", Name: "现浇板", Order: 100, QtyUnit: "块",
		Items: []Qty{
			area("GCL_MJ", "面积", "原始面积"),
			vol("GCL_TJ", "体积", "原始体积"),
			area("GCL_MBMJ", "底面模板面积", "原始底面模板面积"),
			area("GCL_CMMBMJ", "侧面模板面积", "板侧面支模面积"),
			{Code: CodeQuantity, Name: "数量", Unit: UnitCount, Suffix: "块"},
			area("GCL_TYMJ", "投影面积", "原始投影面积"),
			area("GCL_CGMBMJ", "超高模板面积", "原始超高模板面积"),
			area("GCL_CGCMMBMJ", "超高侧面模板面积", "原始超高侧面模板面积"),
			len1("GCL_HD", "板厚", "厚度"),
		},
	},
	{
		ZL: "GJ_LT", Name: "楼梯", Order: 105,
		Items: []Qty{
			area("GCL_TYMJ", "投影面积", "原始投影面积"),
			vol("GCL_TJ", "体积", "原始体积"),
			area("GCL_MBMJ", "模板面积", "原始模板面积"),
		},
	},
	{
		ZL: "GJ_DG", Name: "地沟", Order: 110,
		Items: []Qty{
			len1("GCL_CD", "长度", "计算长度"),
		},
	},
	{
		ZL: "GJ_JZMJ", Name: "建筑面积", Order: 120,
		Items: []Qty{
			area("GCL_YSMJ", "原始面积", ""),
			area("GCL_MJ", "面积", ""),
			len1("GCL_ZC", "周长", ""),
		},
	},
	{
		ZL: "GJ_SS", Name: "散水", Order: 130,
		Items: []Qty{
			area("GCL_MJ", "面积", ""),
			len1("GCL_TQCD", "贴墙长度", ""),
			len1("GCL_WWCD", "外围长度", ""),
			len1("GCL_SSCD", "散水长度", "原始散水长度"),
			area("GCL_MBMJ", "模板面积", "原始模板面积"),
		},
	},
	{
		ZL: "GJ_TJ", Name: "台阶", Order: 135,
		Items: []Qty{
			area("GCL_TYMJ", "投影面积", "原始投影面积"),
			vol("GCL_TJ", "体积", "原始体积"),
		},
	},
	{
		ZL: "GJ_QT_LGFS", Name: "栏杆扶手", Order: 140,
		Items: []Qty{
			len1("GCL_CD", "长度（含弯头）", "长度"),
			len1("GCL_TYCD", "投影长度（含弯头）", "投影长度"),
			area("GCL_MJ", "面积（含弯头）", "原始面积"),
			{Code: "GCL_LGGS", Name: "栏杆根数", Raw: "栏杆根数", Unit: UnitCount, Suffix: "根"},
		},
	},
}

// lxNames 是构件大类编码的兜底中文名（当 ZL 未收录时使用）。
var lxNames = map[string]string{
	"GJ_Z":   "柱",
	"GJ_L":   "梁",
	"GJ_Q":   "墙",
	"GJ_B":   "板",
	"GJ_MCD": "门窗洞",
	"GJ_QT":  "其他",
}

// 不参与计算书输出的子类（钢筋类图元的工程量在 Rebar 分组中，不在本计算书内）。
var skippedZL = map[string]string{
	"GJ_B_BFJ":  "板负筋",
	"GJ_B_BSLJ": "板受力筋",
}

// codeMeta 是单个工程量编码的兜底信息。
type codeMeta struct {
	Name   string
	Unit   Unit
	Suffix string
}

var codeTable = map[string]codeMeta{
	"GCL_ZC":         {"周长", UnitLength, "m"},
	"GCL_TJ":         {"体积", UnitVolume, "m3"},
	"GCL_MJ":         {"面积", UnitArea, "m2"},
	"GCL_MBMJ":       {"模板面积", UnitArea, "m2"},
	"GCL_CGMBMJ":     {"超高模板面积", UnitArea, "m2"},
	"GCL_CMMBMJ":     {"侧面模板面积", UnitArea, "m2"},
	"GCL_CGCMMBMJ":   {"超高侧面模板面积", UnitArea, "m2"},
	"GCL_TYMJ":       {"投影面积", UnitArea, "m2"},
	"GCL_JMMJ":       {"截面面积", UnitArea, "m2"},
	"GCL_LCMMJ":      {"梁侧面面积", UnitArea, "m2"},
	"GCL_JSJMJ":      {"脚手架面积", UnitArea, "m2"},
	"GCL_NQJSJMJ":    {"内墙脚手架面积", UnitArea, "m2"},
	"GCL_WQJSJMJ":    {"外墙脚手架面积", UnitArea, "m2"},
	"GCL_CD":         {"长度", UnitLength, "m"},
	"GCL_GD":         {"高度", UnitLength, "m"},
	"GCL_HD":         {"厚度", UnitLength, "m"},
	"GCL_KD":         {"宽度", UnitLength, "m"},
	"GCL_JMZC":       {"截面周长", UnitLength, "m"},
	"GCL_JMGD":       {"截面高度", UnitLength, "m"},
	"GCL_JMKD":       {"截面宽度", UnitLength, "m"},
	"GCL_LJC":        {"梁净长", UnitLength, "m"},
	"GCL_ZXCD":       {"轴线长度", UnitLength, "m"},
	"GCL_TYCD":       {"投影长度", UnitLength, "m"},
	"GCL_DKMJ":       {"洞口面积", UnitArea, "m2"},
	"GCL_KWWMJ":      {"框外围面积", UnitArea, "m2"},
	"GCL_WJJXDKMJ":   {"外接矩形洞口面积", UnitArea, "m2"},
	"GCL_DKSMCD":     {"洞口三面长度", UnitLength, "m"},
	"GCL_DKKD":       {"洞口宽度", UnitLength, "m"},
	"GCL_DKGD":       {"洞口高度", UnitLength, "m"},
	"GCL_DKZC":       {"洞口周长", UnitLength, "m"},
	"GCL_NBQLGSWPCD": {"内部墙梁钢丝网片长度", UnitLength, "m"},
	"GCL_NBQZGSWPCD": {"内部墙柱钢丝网片长度", UnitLength, "m"},
	"GCL_LGGS":       {"栏杆根数", UnitCount, "根"},
}

// Table 是内置构件类型映射表。
type Table struct {
	byZL     map[string]*TypeSpec
	types    []*TypeSpec
	override map[string]TypeSpec
}

// NewTable 返回带内置映射的规则表。
func NewTable() *Table {
	t := &Table{byZL: map[string]*TypeSpec{}, override: map[string]TypeSpec{}}
	for i := range table {
		cp := table[i]
		t.types = append(t.types, &cp)
		t.byZL[cp.ZL] = &cp
	}
	sort.SliceStable(t.types, func(i, j int) bool { return t.types[i].Order < t.types[j].Order })
	return t
}

// Override 用外部规则覆盖某个子类的定义。返回是否覆盖成功。
func (t *Table) Override(zl string, ts TypeSpec) error {
	if zl == "" {
		return fmt.Errorf("覆盖规则的 ZL 不能为空")
	}
	ts.ZL = zl
	if ts.Order == 0 {
		if old, ok := t.byZL[zl]; ok {
			ts.Order = old.Order
		} else {
			ts.Order = 900
		}
	}
	t.override[zl] = ts
	if ts.Name == "" {
		if old, ok := t.byZL[zl]; ok {
			ts.Name = old.Name
		}
	}
	t.byZL[zl] = &ts
	found := false
	for i, v := range t.types {
		if v.ZL == zl {
			t.types[i] = &ts
			found = true
			break
		}
	}
	if !found {
		t.types = append(t.types, &ts)
	}
	sort.SliceStable(t.types, func(i, j int) bool { return t.types[i].Order < t.types[j].Order })
	return nil
}

// Lookup 返回子类对应的规则；第二个返回值为是否命中内置/覆盖表。
func (t *Table) Lookup(zl string) (*TypeSpec, bool) {
	if s, ok := t.byZL[zl]; ok {
		return s, true
	}
	return nil, false
}

// TypeName 返回子类的中文名。未收录时按「其他（ZL）」命名。
func (t *Table) TypeName(zl string) string {
	if s, ok := t.byZL[zl]; ok {
		return s.Name
	}
	if s, ok := lxNames[prefixLX(zl)]; ok {
		return "其他-" + s
	}
	if zl == "" {
		return "其他"
	}
	return "其他-" + zl
}

// Order 返回子类的类型排序权重，未收录返回 999。
func (t *Table) Order(zl string) int {
	if s, ok := t.byZL[zl]; ok {
		return s.Order
	}
	return 999
}

// Skipped 返回该子类被跳过时的原因；不跳过则返回空串。
func (t *Table) Skipped(zl string) string { return skippedZL[zl] }

// CodeMeta 返回编码的兜底显示名与单位。
func CodeMeta(code string) (string, Unit, string, bool) {
	m, ok := codeTable[code]
	return m.Name, m.Unit, m.Suffix, ok
}

// UnitFromResultSuffix 从 JSSJG 文本反推量纲，用于兜底。
func UnitFromResultSuffix(s string) (Unit, string, bool) { return unitFromSuffix(s) }

// Divisor 返回把 JSL 还原为实数所需的除数。
func Divisor(u Unit) int64 {
	switch u {
	case UnitArea:
		return 1_000_000
	case UnitVolume:
		return 1_000_000_000
	case UnitCount:
		return 1
	default:
		return 1_000
	}
}

// prefixLX 从 ZL 编码中截取大类部分，如 GJ_Z_TZ → GJ_Z。
func prefixLX(zl string) string {
	parts := strings.Split(zl, "_")
	if len(parts) >= 2 {
		return parts[0] + "_" + parts[1]
	}
	return zl
}

// Meta 是覆盖文件（-mapping）的顶层结构。
type Meta struct {
	Types []TypeMeta `json:"types"`
}

// TypeMeta 是覆盖文件中的一个构件类型定义。
type TypeMeta struct {
	ZL      string     `json:"zl"`
	Name    string     `json:"name"`
	Order   int        `json:"order"`
	QtyUnit string     `json:"qtyUnit"`
	Items   []ItemMeta `json:"items"`
}

// ItemMeta 是覆盖文件中的一条工程量定义。
type ItemMeta struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Raw    string `json:"raw"`
	Unit   string `json:"unit"`
	Suffix string `json:"suffix"`
}

// LoadOverrides 从 JSON 文件载入覆盖规则并应用到表中。
func LoadOverrides(t *Table, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取覆盖文件失败: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("解析覆盖文件失败: %w", err)
	}
	for _, tm := range m.Types {
		ts := TypeSpec{ZL: tm.ZL, Name: tm.Name, Order: tm.Order, QtyUnit: tm.QtyUnit}
		old, _ := t.Lookup(tm.ZL)
		for _, im := range tm.Items {
			q := Qty{Code: im.Code, Name: im.Name, Raw: im.Raw}
			if im.Unit != "" {
				u, suffix, ok := unitFromName(im.Unit)
				if !ok {
					return fmt.Errorf("覆盖文件单位无法识别: %q", im.Unit)
				}
				q.Unit, q.Suffix = u, suffix
			} else if old != nil {
				for _, oi := range old.Items {
					if oi.Code == im.Code {
						q.Unit, q.Suffix = oi.Unit, oi.Suffix
						break
					}
				}
			}
			if im.Suffix != "" {
				q.Suffix = im.Suffix
			}
			ts.Items = append(ts.Items, q)
		}
		if ts.Name == "" && old != nil {
			ts.Name = old.Name
		}
		if ts.Order == 0 && old != nil {
			ts.Order = old.Order
		}
		if ts.QtyUnit == "" && old != nil {
			ts.QtyUnit = old.QtyUnit
		}
		if err := t.Override(tm.ZL, ts); err != nil {
			return err
		}
	}
	return nil
}

func unitFromName(name string) (Unit, string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "m", "length", "长度":
		return UnitLength, "m", true
	case "m2", "area", "面积":
		return UnitArea, "m2", true
	case "m3", "volume", "体积":
		return UnitVolume, "m3", true
	case "count", "数量", "根", "樘", "个", "块":
		return UnitCount, name, true
	}
	if _, _, ok := unitFromSuffix(name); ok {
		return unitFromSuffix(name)
	}
	return UnitLength, "", false
}

// FloorName 按楼层号（LCH）生成楼层名，与真实文件一致（1 = 首层）。
func FloorName(lch int) string {
	if lch == 1 {
		return "首层"
	}
	if lch > 1 {
		if lch <= 10 {
			return cnNumber(lch) + "层"
		}
		return fmt.Sprintf("%d层", lch)
	}
	return "地下" + cnNumber(1-lch) + "层"
}

var cnDigits = []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}

func cnNumber(n int) string {
	switch {
	case n < 0:
		return fmt.Sprintf("%d", n)
	case n < 10:
		return cnDigits[n]
	case n == 10:
		return "十"
	case n < 20:
		return "十" + cnDigits[n%10]
	case n < 100:
		s := cnDigits[n/10] + "十"
		if n%10 != 0 {
			s += cnDigits[n%10]
		}
		return s
	default:
		return fmt.Sprintf("%d", n)
	}
}

// SectionName 返回计算结果分组的显示名。
func SectionName(key string) string {
	switch strings.ToLower(key) {
	case "list":
		return "List"
	case "norm":
		return "Norm"
	}
	return key
}

// SectionOf 按名称从文档中取出分组。
func SectionOf(doc *model.Document, key string) *model.Section {
	switch strings.ToLower(key) {
	case "list":
		return &doc.List
	default:
		return &doc.Norm
	}
}
