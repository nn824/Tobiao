// Package grid 提供轴网模型，以及「平面坐标 → 轴号写法」的换算。
//
// 计算书里的「图元位置」列写成 <轴号,轴号>，轴号后面可以直接跟相对该轴线的
// 偏移量（单位 mm）：
//
//	<6,D>             轴 6 与轴 D 的交点
//	<4-150,A+150>     轴 4 往小里程 150，轴 A 往 E 方向 150
//	<10+2400,E-300>   轴 10 往大里程 2400，轴 E 往 D 方向 300
//	<11+500,D+3550>   已经出到轴 11 之外 500（散水等室外构件）
//
// 约定：
//   - 横向用图纸上的数字轴号，偏移的正方向就是 x 增大的方向；
//   - 纵向用字母轴号 A..E，内部把坐标换成 w = -z（A 为 0，向 E 递增），
//     偏移的正方向是 w 增大的方向；因此 z 越负越靠 E，写出来就是「E-300」。
//
// 空字符串表示没有可用的轴网或坐标，调用方应据此跳过该单元格。
package grid

import (
	"fmt"
	"math"
	"strings"
)

// Axis 是一条轴线。
type Axis struct {
	Label string  `json:"label"` // 轴号，如 "6"、"D"
	Pos   float64 `json:"pos"`   // 坐标：横向为 x；纵向为从起始轴（A）量起的距离
}

// Grid 是轴网。X 是横向轴（数字轴号），Y 是纵向轴（字母轴号，按 A→G 排列）。
type Grid struct {
	X []Axis `json:"x"`
	Y []Axis `json:"y"`
}

// Point 是楼层平面上的一个点（平面用 x/z 两轴，楼层高度方向为 y，此处不参与）。
type Point struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
}

// XAxes / YAxes 是默认轴网（智多星首层，总长 75400 × 总宽 30000），
// 轴距依次为 9900、9000、9000、5400、800、7800、5600、9000、9000、9900
// 与 4500、8700、8400、8400。
var (
	XAxes = []Axis{
		{Label: "1", Pos: 0},
		{Label: "2", Pos: 9900},
		{Label: "3", Pos: 18900},
		{Label: "4", Pos: 27900},
		{Label: "5", Pos: 28300},
		{Label: "6", Pos: 34100},
		{Label: "7", Pos: 41900},
		{Label: "8", Pos: 47500},
		{Label: "9", Pos: 56500},
		{Label: "10", Pos: 65500},
		{Label: "11", Pos: 75400},
	}
	YAxes = []Axis{
		{Label: "A", Pos: 0},
		{Label: "B", Pos: 4500},
		{Label: "C", Pos: 13200},
		{Label: "D", Pos: 21600},
		{Label: "E", Pos: 30000},
	}
)

// Default 返回默认轴网的一个副本。
func Default() *Grid {
	return &Grid{
		X: append([]Axis(nil), XAxes...),
		Y: append([]Axis(nil), YAxes...),
	}
}

// Label 把平面坐标 (x, z) 换算成 <轴,轴> 写法。
// 轴网为空时返回空字符串。
func (g *Grid) Label(x, z float64) string {
	if g == nil || (len(g.X) == 0 && len(g.Y) == 0) {
		return ""
	}
	return "<" + axisLabel(g.X, x) + "," + axisLabel(g.Y, -z) + ">"
}

// Labels 把一串特征点换算成连续写法，例如 <1,E-50><2+3000,E-50>。
func (g *Grid) Labels(pts []Point) string {
	if g == nil || len(pts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range pts {
		b.WriteString(g.Label(p.X, p.Z))
	}
	return b.String()
}

// axisLabel 找出离 v 最近的轴线，并把它表示成「轴号 ± 偏移」。
func axisLabel(axes []Axis, v float64) string {
	if len(axes) == 0 {
		return fmt.Sprintf("%d", int(math.Round(v)))
	}
	best := axes[0]
	bestDist := math.Abs(v - best.Pos)
	for _, a := range axes[1:] {
		if d := math.Abs(v - a.Pos); d < bestDist {
			best, bestDist = a, d
		}
	}
	offset := int(math.Round(v - best.Pos))
	if offset == 0 {
		return best.Label
	}
	return fmt.Sprintf("%s%+d", best.Label, offset)
}
