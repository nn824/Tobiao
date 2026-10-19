// Package geom 读取智多星导出的「三维模型」JSON（含图元坐标），
// 为每个图元提取用于填写「图元位置」列的特征点。
//
// 为什么需要额外的模型文件：计算结果的 JSON 只有算量、没有坐标；
// 同一个工程另有一份模型导出，结构如下：
//
//	{ "ZGCS": [ { "LCS": [ { "component": [ { "@type": "...", ... }, ... ] } ] } ] }
//
// 每个 component 的 @type 决定几何载体：
//
//	ConcreteColumnRectangle   <构件>.concreteColumn.pointComponent.matrix
//	RectDoor / RectWindow     <构件>.Door|window.pointComponent.matrix（另有 DKKD 洞口宽度）
//	MasonryWallRectangle      <构件>.wall.line
//	BeamRectangle             <构件>.beam.KS[].path[].line（多跨就是多条线）
//	RingBeamRect              <构件>.ringBeam.line
//	LintelRect                <构件>.lintel.line
//	HandrailWithBaluster      <构件>.handrail.line
//	Slab                      <构件>.newSection.outline[].edge（回退 section.values[]）
//
// 门窗是特例：矩阵给出的是洞口的一个参照角点，计算书里报的是洞口沿墙方向的
// 中点，所以要沿洞口宽度方向再偏移「洞口宽度 / 2」。方向由穿过该点的宿主墙线
// 判定（实测 25/25 与参照计算书一致）。
package geom

import (
	"fmt"
	"math"
	"sort"

	"github.com/nn824/Tobiao/internal/grid"
	"github.com/nn824/Tobiao/internal/safejson"
)

// Segment 是模型里的一段线（墙中心线、梁中心线、板轮廓边等）。
type Segment struct {
	X1, Z1 float64
	X2, Z2 float64
}

// Entry 是一个模型图元条目。
type Entry struct {
	CID    string       // 构件定义 id（同一类构件共用）
	PID    string       // 构件实例 id（每个图元唯一，是与计算结果对齐的首选键）
	Kind   string       // @type 的最后一段，如 ConcreteColumnRectangle
	Points []grid.Point // 特征点；没有坐标时为空
	Width  float64      // 洞口宽度（仅门窗）
}

// Model 是解析后的模型。
//
// 与计算结果文件的对齐：PID（构件实例 id）在两份文件里都是 589 个互不相同的
// 值且集合完全一致，因此按 PID 可以做到严格 1:1。CID 只区分「构件类型」，
// 139 个图元共用 141 个 CID 中的一部分，只能作为没有 PID 时的兜底。
type Model struct {
	entries []Entry
	byCID   map[string][]int
	byPID   map[string][]int
	segs    []Segment
}

// Count 返回模型中的图元条目数。
func (m *Model) Count() int { return len(m.entries) }

// Load 读取并解析模型 JSON 文件。
func Load(path string) (*Model, error) {
	var doc map[string]any
	if err := safejson.ParseFile(path, &doc); err != nil {
		return nil, fmt.Errorf("geom: 读取模型文件 %s 失败: %w", path, err)
	}
	m, err := parseRoot(doc)
	if err != nil {
		return nil, fmt.Errorf("geom: 解析模型文件 %s 失败: %w", path, err)
	}
	return m, nil
}

// Parse 解析模型 JSON 字节内容。
func Parse(data []byte) (*Model, error) {
	var doc map[string]any
	if err := safejson.Parse(data, &doc); err != nil {
		return nil, fmt.Errorf("geom: 解析模型 JSON 失败: %w", err)
	}
	return parseRoot(doc)
}

func parseRoot(doc map[string]any) (*Model, error) {
	comps := collectComponents(doc)
	if len(comps) == 0 {
		return nil, fmt.Errorf("未在模型中找到 ZGCS[].LCS[].component 图元列表")
	}
	m := &Model{byCID: map[string][]int{}, byPID: map[string][]int{}}
	for _, c := range comps {
		ent := buildEntry(c)
		m.byCID[ent.CID] = append(m.byCID[ent.CID], len(m.entries))
		if ent.PID != "" {
			m.byPID[ent.PID] = append(m.byPID[ent.PID], len(m.entries))
		}
		m.entries = append(m.entries, ent)
	}
	for _, c := range comps {
		collectSegments(c, &m.segs)
	}
	return m, nil
}

// Resolver 把图元换算成「图元位置」文本。
//
// 优先按 PID（构件实例 id）精确取值：它在两份文件里一一对应，不会串位。
// 取不到 PID 时退化为按 CID 分组、按调用顺序取第 k 条。
type Resolver struct {
	byPID     map[string][]string
	byCID     map[string][]string
	cursorPID map[string]int
	cursorCID map[string]int
}

// Position 返回该图元的位置文本；没有对应坐标时返回空字符串。
func (r *Resolver) Position(cid, pid string) string {
	if r == nil {
		return ""
	}
	if pid != "" {
		if list := r.byPID[pid]; len(list) > 0 {
			k := r.cursorPID[pid]
			if k < len(list) {
				r.cursorPID[pid] = k + 1
				return list[k]
			}
		}
	}
	if cid == "" {
		return ""
	}
	list := r.byCID[cid]
	k := r.cursorCID[cid]
	if k >= len(list) {
		return ""
	}
	r.cursorCID[cid] = k + 1
	return list[k]
}

// Resolver 为模型生成一个位置解析器。g 为轴网；为 nil 时使用内置轴网。
func (m *Model) Resolver(g *grid.Grid) *Resolver {
	if g == nil {
		g = grid.Default()
	}
	r := &Resolver{
		byPID:     map[string][]string{},
		byCID:     map[string][]string{},
		cursorPID: map[string]int{},
		cursorCID: map[string]int{},
	}
	for _, e := range m.entries {
		label := m.labelOf(e, g)
		if e.PID != "" {
			r.byPID[e.PID] = append(r.byPID[e.PID], label)
		}
		r.byCID[e.CID] = append(r.byCID[e.CID], label)
	}
	return r
}

// labelOf 把一个条目换算成位置文本，必要时先做门窗洞口偏移。
func (m *Model) labelOf(e Entry, g *grid.Grid) string {
	pts := append([]grid.Point(nil), e.Points...)
	if (e.Kind == kindDoor || e.Kind == kindWindow) && len(pts) == 1 && e.Width > 0 {
		half := e.Width / 2
		if seg, ok := m.hostSegment(pts[0]); ok {
			if math.Abs(seg.X2-seg.X1) >= math.Abs(seg.Z2-seg.Z1) {
				pts[0].X += half // 洞口在沿 x 的墙上 → 沿 x 偏移
			} else {
				pts[0].Z -= half // 洞口在沿 z 的墙上 → 沿 w = -z 偏移
			}
		} else if onAxisX(g, pts[0].X) <= onAxisW(g, pts[0].Z) {
			pts[0].Z -= half
		} else {
			pts[0].X += half
		}
	}
	// 线状构件只写起止两点：多跨梁、多段钢筋在计算结果里是一个图元，
	// 写全所有折点会被拆成若干段，与计算书「<起点><终点>」的写法不符。
	if len(pts) > 2 {
		pts = []grid.Point{pts[0], pts[len(pts)-1]}
	}
	if len(pts) == 2 && samePoint(pts[0], pts[1]) {
		pts = pts[:1]
	}
	return g.Labels(pts)
}

// samePoint 判断两个点是否重合（1mm 以内视为同一点）。
func samePoint(a, b grid.Point) bool {
	return math.Abs(a.X-b.X) < 1 && math.Abs(a.Z-b.Z) < 1
}

// stretchTo 把两点线段按设计长度补足。
// length 为 0（没有该字段）或线段已经够长时原样返回。
func stretchTo(pts []grid.Point, length float64) []grid.Point {
	if len(pts) != 2 || length <= 0 {
		return pts
	}
	dx, dz := pts[1].X-pts[0].X, pts[1].Z-pts[0].Z
	cur := math.Hypot(dx, dz)
	if cur <= 0 || length <= cur+0.5 {
		return pts
	}
	k := length / cur
	return []grid.Point{pts[0], {X: pts[0].X + dx*k, Z: pts[0].Z + dz*k}}
}

// designLength 取构件上的「长度」字段（CD）。
func designLength(node any) float64 {
	v, _ := number(asMap(node)["CD"])
	return v
}

// onAxisX / onAxisW 返回坐标到最近轴线的距离，用于兜底判断墙的走向。
func onAxisX(g *grid.Grid, x float64) float64 {
	best := math.Inf(1)
	for _, a := range g.X {
		if d := math.Abs(x - a.Pos); d < best {
			best = d
		}
	}
	return best
}

func onAxisW(g *grid.Grid, z float64) float64 {
	best := math.Inf(1)
	for _, a := range g.Y {
		if d := math.Abs(-z - a.Pos); d < best {
			best = d
		}
	}
	return best
}

// hostSegment 找出穿过 p 的宿主线段：先要求严格穿过（≤1mm），
// 再放宽到 300mm；都找不到就返回 false。
func (m *Model) hostSegment(p grid.Point) (Segment, bool) {
	for _, tol := range []float64{1, 300} {
		best := Segment{}
		bestDist := math.Inf(1)
		found := false
		for _, s := range m.segs {
			d, ok := distanceToSegment(p.X, p.Z, s)
			if !ok || d > tol {
				continue
			}
			if d < bestDist {
				best, bestDist, found = s, d, true
			}
		}
		if found {
			return best, true
		}
	}
	return Segment{}, false
}

// distanceToSegment 返回点到线段的距离；点落在延长线之外时返回 ok=false。
func distanceToSegment(px, pz float64, s Segment) (float64, bool) {
	dx, dz := s.X2-s.X1, s.Z2-s.Z1
	l2 := dx*dx + dz*dz
	if l2 == 0 {
		return math.Hypot(px-s.X1, pz-s.Z1), true
	}
	t := ((px-s.X1)*dx + (pz-s.Z1)*dz) / l2
	if t < -0.02 || t > 1.02 {
		return 0, false
	}
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(s.X1+t*dx), pz-(s.Z1+t*dz)), true
}

// ---------------------------------------------------------------- 条目解析

const (
	kindDoor     = "RectDoor"
	kindWindow   = "RectWindow"
	kindColumn   = "ConcreteColumnRectangle"
	kindWall     = "MasonryWallRectangle"
	kindBeam     = "BeamRectangle"
	kindRing     = "RingBeamRect"
	kindLintel   = "LintelRect"
	kindHandrail = "HandrailWithBaluster"
	kindSlab     = "Slab"
)

func buildEntry(comp map[string]any) Entry {
	kind := lastSegment(stringOf(comp["@type"]))
	cid, pid := idPair(comp)
	e := Entry{CID: cid, PID: pid, Kind: kind}

	switch kind {
	case kindColumn, kindDoor, kindWindow:
		if p, ok := firstMatrix(comp); ok {
			e.Points = []grid.Point{p}
		}
		if kind == kindDoor || kind == kindWindow {
			e.Width, _ = number(comp["DKKD"])
		}
	case kindWall:
		e.Points = linesUnder(comp["wall"])
	case kindBeam:
		e.Points = linesUnder(comp["beam"])
	case kindRing:
		e.Points = linesUnder(comp["ringBeam"])
	case kindLintel:
		// 洞口边缘的过梁，模型里的线有时会被墙端截断（终点落在整数坐标上），
		// 而计算书报的是设计长度 CD，这里以起点为基准沿原方向补足。
		e.Points = stretchTo(linesUnder(comp["lintel"]), designLength(comp["lintel"]))
	case kindHandrail:
		e.Points = linesUnder(comp["handrail"])
	case kindSlab:
		// 板的 section.values / newSection.outline 在部件同级，不在 section 里
		e.Points = polygonCentroid(comp["newSection"])
		if len(e.Points) == 0 {
			e.Points = polygonCentroid(comp["section"])
		}
	default:
		if p, ok := firstMatrix(comp); ok {
			e.Points = []grid.Point{p}
		} else {
			e.Points = linesUnder(comp)
		}
	}
	e.Points = dedupe(e.Points)
	return e
}

// linesUnder 按出现顺序收集 node 下的所有 {start,end} 线段端点。
// 只对根节点的键递归，避免把无关的深层线段（配筋等）混进来。
func linesUnder(node any) []grid.Point {
	var out []grid.Point
	var walk func(v any, root bool)
	walk = func(v any, root bool) {
		switch t := v.(type) {
		case map[string]any:
			if ln, ok := t["line"].(map[string]any); ok {
				if s, sok := pointOf(ln["start"]); sok {
					out = append(out, s)
				}
				if en, eok := pointOf(ln["end"]); eok {
					out = append(out, en)
				}
				return
			}
			for k, child := range t {
				if root && k == "line" {
					continue
				}
				walk(child, false)
			}
		case []any:
			for _, child := range t {
				walk(child, false)
			}
		}
	}
	walk(node, true)
	return out
}

// polygonCentroid 取轮廓的形心；轮廓退化（面积≈0）时退化为顶点平均。
func polygonCentroid(node any) []grid.Point {
	verts := polygonVertices(node)
	if len(verts) < 3 {
		return nil
	}
	var area, cx, cz float64
	for i := range verts {
		j := (i + 1) % len(verts)
		cross := verts[i].X*verts[j].Z - verts[j].X*verts[i].Z
		area += cross
		cx += (verts[i].X + verts[j].X) * cross
		cz += (verts[i].Z + verts[j].Z) * cross
	}
	area /= 2
	if math.Abs(area) < 1e-6 {
		var sx, sz float64
		for _, v := range verts {
			sx += v.X
			sz += v.Z
		}
		n := float64(len(verts))
		return []grid.Point{{X: sx / n, Z: sz / n}}
	}
	return []grid.Point{{X: cx / (6 * area), Z: cz / (6 * area)}}
}

// polygonVertices 收集轮廓顶点：优先 outline[].edge，其次 values[]。
func polygonVertices(node any) []grid.Point {
	obj, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	if outline, ok := obj["outline"].([]any); ok {
		var pts []grid.Point
		for _, item := range outline {
			edge, _ := asMap(item)["edge"]
			em, ok := edge.(map[string]any)
			if !ok {
				continue
			}
			if p, ok := pointOf(em["start"]); ok {
				pts = append(pts, p)
			}
		}
		if len(pts) >= 3 {
			return dedupe(pts)
		}
	}
	if values, ok := obj["values"].([]any); ok {
		var pts []grid.Point
		for _, item := range values {
			if p, ok := pointOf(asMap(item)["start"]); ok {
				pts = append(pts, p)
			}
		}
		if len(pts) >= 3 {
			return dedupe(pts)
		}
	}
	return nil
}

// dedupe 去掉相邻的重复点（多跨构件在跨与跨之间会重复端点）。
func dedupe(pts []grid.Point) []grid.Point {
	out := pts[:0:0]
	for _, p := range pts {
		if n := len(out); n > 0 && math.Abs(out[n-1].X-p.X) < 0.01 && math.Abs(out[n-1].Z-p.Z) < 0.01 {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ---------------------------------------------------------------- JSON 辅助

func collectComponents(root map[string]any) []map[string]any {
	var out []map[string]any
	for _, zg := range listOf(root["ZGCS"]) {
		for _, lc := range listOf(asMap(zg)["LCS"]) {
			for _, c := range listOf(asMap(lc)["component"]) {
				if m, ok := c.(map[string]any); ok {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

func collectSegments(node any, out *[]Segment) {
	switch t := node.(type) {
	case map[string]any:
		s1, ok1 := pointOf(t["start"])
		s2, ok2 := pointOf(t["end"])
		if ok1 && ok2 {
			*out = append(*out, Segment{X1: s1.X, Z1: s1.Z, X2: s2.X, Z2: s2.Z})
			return
		}
		for _, v := range t {
			collectSegments(v, out)
		}
	case []any:
		for _, v := range t {
			collectSegments(v, out)
		}
	}
}

// idPair 找出同时带 cid 与 pid 的那个子对象，返回它的两个 id。
// 智多星把「构件定义 id」和「构件实例 id」成对放在 component 里，
// 而梁的支座、板的归属等引用只给单个 uuid，因此要求成对命中才认。
// 键按字典序遍历，保证同一份文件的解析结果稳定。
func idPair(node any) (cid, pid string) {
	switch t := node.(type) {
	case map[string]any:
		if c, ok := t["cid"].(string); ok && c != "" {
			if p, ok := t["pid"].(string); ok && p != "" {
				return c, p
			}
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if c, p := idPair(t[k]); c != "" {
				return c, p
			}
		}
	case []any:
		for _, v := range t {
			if c, p := idPair(v); c != "" {
				return c, p
			}
		}
	}
	return "", ""
}

func firstMatrix(node any) (grid.Point, bool) {
	switch t := node.(type) {
	case map[string]any:
		if m, ok := t["matrix"].([]any); ok && len(m) >= 12 {
			x, okx := number(m[3])
			z, okz := number(m[11])
			if okx && okz {
				return grid.Point{X: x, Z: z}, true
			}
		}
		for _, v := range t {
			if p, ok := firstMatrix(v); ok {
				return p, true
			}
		}
	case []any:
		for _, v := range t {
			if p, ok := firstMatrix(v); ok {
				return p, true
			}
		}
	}
	return grid.Point{}, false
}

func pointOf(node any) (grid.Point, bool) {
	m, ok := node.(map[string]any)
	if !ok {
		return grid.Point{}, false
	}
	x, okx := number(m["x"])
	z, okz := number(m["z"])
	if !okx || !okz {
		return grid.Point{}, false
	}
	return grid.Point{X: x, Z: z}, true
}

func asMap(node any) map[string]any {
	if m, ok := node.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func listOf(node any) []any {
	if l, ok := node.([]any); ok {
		return l
	}
	return nil
}

func number(node any) (float64, bool) {
	switch v := node.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func stringOf(node any) string {
	s, _ := node.(string)
	return s
}

func lastSegment(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' || s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}
