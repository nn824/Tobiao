# zhiduoxing2xls

把智多星导出的工程量计算结果 JSON，转换成与 `新.xls` **同版式**的
「绘图输入构件工程量计算书」`.xls` 文件。

- **纯 Go 实现，零第三方依赖**：BIFF8（Excel 97-2003）与 OLE2 复合文档容器都是自己写的，
  `go build` 不需要联网拉包。
- **版式由真实文件反推**：列宽、行高公式、合并区、字体、边框、对齐全部以
  智多星原生导出的计算书逐条比对得出，并用 xlrd 做了逐项一致性校验。
- **容错解析**：源 JSON 带 BOM、尾随逗号、注释、代码围栏、单引号，或外面包着日志文字，
  都能正常解析（对应 `json-utils` 与 `dmn20264958` 两个技能的语义）；解析失败返回错误而不是崩溃。

## 在自己的 Go 项目里调用（推荐）

导入根包 `zhiduoxingxls`，**入参就是测试结果的 JSON 文件路径**：

```go
import zhiduoxingxls "github.com/nn824/Tobiao"

func main() {
    // 最短写法：只传 JSON 文件路径，输出同名 .xls
    rep, err := zhiduoxingxls.Convert("首层砼柱测试结果.json",
        zhiduoxingxls.Options{Overwrite: true})
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("图元 %d 个、分组 %d 个 → %s", rep.Components, rep.Groups, rep.Out)
}
```

按需指定参数：

```go
rep, err := zhiduoxingxls.Convert(jsonPath, zhiduoxingxls.Options{
    Out:       "out/计算书.xls",       // 留空 = 与入参同名的 .xls
    Section:   zhiduoxingxls.SectionNorm,
    Floor:     "首层",
    SheetName: "绘图输入构件工程量计算书-首层",
    Mapping:   "",                     // 规则覆盖文件，留空用内置规则
    ModelPath: "模型.json",             // 智多星导出的三维模型，用来自动填「图元位置」
    AxesPath:  "轴网.json",             // 轴网：换工程时只改这一个入参
    // Axes:    &zhiduoxingxls.Axes{X: ..., Y: ...}, // 想在代码里写死时用（优先级更高）
    // 都不给 → 用内置轴网（智多星首层 1..11 / A..E）
    Positions: map[string]string{"c1": "<6,D>"}, // 手填对照表，优先级最高
    Overwrite: true,
})
```

### 安装

```bash
go get github.com/nn824/Tobiao
```

```go
import zhiduoxingxls "github.com/nn824/Tobiao"
```

**没有任何第三方依赖**，不会给你的项目引入额外的供应链；`go.sum` 保持为空。
需要锁定版本时用 `go get github.com/nn824/Tobiao@v0.1.0`。

<details>
<summary>想直接引用本地副本（尚未发布 tag 时）</summary>

先 `cd` 到**你自己项目的 `go.mod` 所在目录**（`go mod edit` 只认当前目录的 `go.mod`，
目录里没有它会报 `go: open .\go.mod: The system cannot find the file specified.`），然后：

```bash
go mod edit -require=github.com/nn824/Tobiao@v0.0.0
go mod edit -replace=github.com/nn824/Tobiao=../zhiduoxing-xls
go mod tidy
```

> **路径写法有两个坑，务必注意：**
>
> 1. **不要带前导 `/`**。`replace` 右侧只要以 `/` 开头，Go 会当成**相对路径**，
>    再拼上当前目录，于是报
>    `CreateFile D:\你的项目\E:\...\zhiduoxing-xls: The filename, directory name, or volume label syntax is incorrect.`
>    正确形式是 `E:/...`、`E:\...`，或纯相对路径 `../zhiduoxing-xls`。
> 2. **统一用正斜杠 `/`**，或给整条路径加引号。反斜杠在 PowerShell 里容易被吞掉
>    或被 `go mod edit` 二次转义。
>
> 执行完用 `go env GOMOD` 确认改的是哪个 `go.mod`，再 `grep zhiduoxing go.mod` 看结果。

</details>

### 公开 API

| 符号 | 说明 |
| --- | --- |
| `Convert(jsonPath string, opts Options) (*Report, error)` | **主入口**：读 JSON 文件 → 写出 `.xls` |
| `ConvertBytes(data []byte, opts Options) ([]byte, *Report, error)` | 输入来自内存/网络流时用，返回 `.xls` 字节流，不落盘 |
| `Compare(opts CompareOptions) (*CompareReport, error)` | **比对**：两份计算书按「楼层 + 构件名 + 坐标」逐条比工程量计算式 |
| `Options` | 转换参数，零值即可用（Norm 分组、同名输出、输出数量行、不覆盖已存在文件） |
| `CompareOptions` | 比对参数：`Generated`（计算书）、`Reference`（参照文件）、`ResultJSON`（取楼层名） |
| `CompareReport` | 比对结果：`Same`、`Diff`、`OnlyGenerated`、`OnlyReference` 等计数 |
| `Report` | 转换结果与诊断信息：`Out`、`SheetName`、`Components`、`Groups`、`Rows`、`Warnings` 等 |
| `ErrEmptyInput` / `ErrParseJSON` / `ErrOutputExists` | 哨兵错误，可用 `errors.Is` 判断 |
| `SectionNorm` / `SectionList` | 计算结果分组常量 |

设计上的几个约定：

- **不覆盖已有文件**：`Options.Overwrite` 为 `false` 时目标已存在会返回 `ErrOutputExists`，
  避免误删数据。CLI 里对应的开关是 `-y`。
- **原子写出**：先写同目录临时文件再改名，中途失败不会留下半个文件。
- **不 panic**：JSON 再脏也只是返回错误，配合 `ErrParseJSON` 判断即可。

本仓库的单元测试覆盖了 JSON 容错解析、BIFF8 写出/读回往返、轴网换算、比对状态机。
克隆后想快速确认能跑通：

```bash
go test ./...
go run ./cmd/zhiduoxing2xls 结果.json -out 计算书.xls -y
```

## 命令行用法

```bash
go build -o zhiduoxing2xls ./cmd/zhiduoxing2xls

# JSON 文件可直接作为位置参数
./zhiduoxing2xls 智多星首层砼柱测试结果json.txt -out 智多星首层砼柱计算书.xls -y

# 也支持传统的 -in 写法
./zhiduoxing2xls -in 结果.txt -out 计算书.xls -y

# 比对两份计算书（子命令 compare）
./zhiduoxing2xls compare 新工程计算书.xls 新.xls 测试结果.json -y
```

CLI 只是公开包的薄封装，逻辑与 `Convert` / `Compare` 完全一致。

### 参数

| 参数 | 说明 |
| --- | --- |
| *位置参数* 或 `-in` | 输入 JSON 文件路径（必填，两者选一） |
| `-out` | 输出 `.xls` 路径；省略时与输入同名 |
| `-section` | 使用哪个计算结果分组：`norm`（默认）或 `list` |
| `-floor` | 楼层名，如「首层」；留空按楼层号推导（`LCH=1` → 首层） |
| `-sheet` | 工作表名；留空时生成「绘图输入构件工程量计算书-楼层」 |
| `-mapping` | 规则覆盖文件（JSON），修正量名 / 单位 / 顺序 |
| `-model` | 智多星导出的三维模型 JSON（含坐标），给出后自动填「图元位置」列 |
| `-axes` | **换工程时只改这个**：轴网 JSON 文件，支持智多星原生格式与 `{x,y}` 简写格式 |
| `-dump-axes` | 导出内置轴网 JSON 模板后退出，供照图纸改 |
| `-append-extra` | 把类型规则之外的工程量追加到明细末尾（默认关闭，与参照文件一致） |
| `-emit-qty` | 是否输出「数量」行（默认 `true`；JSON 未导出该量，按图元恒为 1 生成） |
| `-quiet` | 只写文件，不打印统计 |
| `-y` | 允许覆盖已存在的输出文件 |

### 补全「图元位置」列

计算结果 JSON 里没有坐标（`Shapes` 为空、无轴网字段），该列默认留空。三种补法，
优先级从高到低：

**① 三维模型自动换算（推荐）**

智多星「导出三维模型」得到的 JSON 里带每个图元的坐标。在**同一次导出**里同时
拿到「计算结果」和「三维模型」两份文件，把模型路径传给 `-model` / `Options.ModelPath`，
程序会按轴网把坐标换算成 `<轴,轴>` 写法：

```bash
./zhiduoxing2xls 测试结果json.txt -model 三维模型.json -out 计算书.xls -y
```

换算是这样做的：

- **对齐**：两份文件各 589 个图元，`PID`（构件实例 id）互不相同且集合完全一致，
  按 PID 可 1:1 对上；没有 PID 时退化为按 `CID` 分组、按调用顺序取第 k 条。
- **点式构件**（柱 / 构造柱 / 门 / 窗 / 板）：取几何中心（板取轮廓形心）。
- **门窗**：模型给的是洞口的一个角点，计算书报的是洞口沿墙方向的中点，
  因此沿洞口宽方向再偏移 `DKKD/2`；方向取穿过该点的宿主墙线走向。
- **线式构件**（梁 / 墙 / 圈梁 / 过梁 / 栏杆 / 钢筋）：写 `<起点><终点>`。
  多跨梁在计算结果里是一个图元，跨与跨的折点会被收敛掉，只保留首尾两点 ——
  与计算书「一个单元格最多两组 `<>`」的写法一致。
- **过梁**：模型里的线有时在墙端被截断，此时按构件自带的 `CD`（设计长度）
  以起点为基准补足。
- **轴网**：把坐标换算成 `<轴号,轴号>`，内部把 z 换成 `w = -z`。
  换工程时用 `-axes 轴网.json` 传入（详见《换一套轴网》），
  不给则用内置的智多星首层轴网（横向 1..11 / 纵向 A..E）。

**② 手工对照表**：把 `Options.Positions`（`CID → 位置`）传给 API，
或在输入文件同目录放一份 `<输入文件名>.positions.csv`：

```csv
CID,位置
c1,"<6,D>"
c2,"<7,D>"
```

> 位置值形如 `<6,D>`，**本身含逗号**，因此务必加引号；解析用的是标准 CSV 而非按逗号切分，
> 加引号后不会截断。若文件里出现制表符则按 TSV 解析。

**③ 留空**：不传任何定位信息时该列全空，其余内容照常输出。

### 换一套轴网

**内置轴网写死在智多星首层**（横向 `1..11` 总长 75400、纵向 `A..E` 总宽 30000）。
换一个工程若不改，算出来的轴号就是错的 —— 这是最容易踩的坑。

**推荐做法：把智多星导出的轴网文件直接用 `-axes` 传进来**，不用改代码、不用转换格式：

```bash
./zhiduoxing2xls -in 结果.json -model 模型.json -axes 轴网.json -out 计算书.xls -y
```

支持两种格式，按文件第一个字符（`[` 还是 `{`）自动识别：

**① 智多星原生轴网文件**（导出即可用，最省事）：

```json
[
  {"axis":{"axisLabel":"A"},"distance":0,   "direction":"horizontal"},
  {"axis":{"axisLabel":"B"},"distance":6600,"direction":"horizontal"},
  {"axis":{"axisLabel":"1"},"distance":0,   "direction":"vertical"},
  {"axis":{"axisLabel":"2"},"distance":6600,"direction":"vertical"}
]
```

- `horizontal`（水平轴线，字母轴号）→ 纵向；`vertical`（垂直轴线，数字轴号）→ 横向；
- `distance` 是该轴到起始轴的距离，单位 mm；
- 左右 / 上下重复出现的同一条轴会自动去重；支持附加轴 `1/1`、`1/E`、`2A`。

**② 简写格式**（`pos` 为绝对坐标 mm，横向取模型 x，纵向取 `-z`）：

```json
{"x": [{"label":"1","pos":0}, {"label":"2","pos":9900}],
 "y": [{"label":"A","pos":0}, {"label":"B","pos":4500}]}
```

两种格式里条目顺序都无所谓，会按 `pos` 自动升序。不带 `-axes` 时用内置轴网；
想照图纸手改可先导出模板：

```bash
./zhiduoxing2xls -dump-axes 轴网.json
```

Go 里对应 `Options.AxesPath: "轴网.json"`，也可先
`a, err := zhiduoxingxls.LoadAxes("轴网.json")` 检查后再传给 `Options.Axes`。
运行时 `Report.Axes` 会打印本次实际用的轴网：

```
轴网      : 轴网.json（横向 11 轴、纵向 10 轴）
```


#### 备选：代码里直接给（不想多带一个文件）

```go
rep, err := zhiduoxingxls.Convert(jsonPath, zhiduoxingxls.Options{
    ModelPath: "模型.json",
    Axes: &zhiduoxingxls.Axes{
        X: []zhiduoxingxls.Axis{
            {Label: "1", Pos: 0}, {Label: "2", Pos: 9900}, {Label: "3", Pos: 18900},
        },
        Y: []zhiduoxingxls.Axis{
            {Label: "A", Pos: 0}, {Label: "B", Pos: 4500},
        },
    },
})
```

轴号可以直接用 `DefaultAxes()` 起手再改：

```go
a := zhiduoxingxls.DefaultAxes()
a.X = append(a.X[:5], zhiduoxingxls.Axis{Label: "5A", Pos: 29000})
```

#### 备选：改源码常量（整个项目只服务这一个工程）

`internal/grid/grid.go` 里第 46–67 行的 `XAxes` / `YAxes`：

```go
XAxes = []Axis{
    {Label: "1", Pos: 0},
    {Label: "2", Pos: 9900},
    ...
}
YAxes = []Axis{
    {Label: "A", Pos: 0},
    {Label: "B", Pos: 4500},
    ...
}
```

改完记得跑一次 `go test ./...` —— `TestDefaultAxes` 会锁住旧值，需要同步更新。

#### `pos` 到底填什么

`pos` 是**绝对坐标**，单位 mm，不是轴距差：

- **横向（x）**：直接取模型里的 `x` 坐标。图纸越靠右数值越大。
- **纵向（y）**：取「距起始轴的距离」。模型里的楼层平面是 `(x, z)`，
  `z ≤ 0`，代码内部换算成 `w = -z`（所以 `z = -30000` 就是 `w = 30000`）。
  也就是说 **`Y` 里第一个轴（A）的 pos 通常是 0**，越往 `E` 越大。

两个方向都必须**从同一原点量起**（A/1 轴交点惯例是 `(0, 0)`）。
如果模型整体有偏移（比如所有 x 都加了 1000），把 Axes 里所有 pos 同步加 1000 即可，
不要动模型文件。

其它约束：

- 录入**顺序无所谓**，会自动按 `pos` 升序排序（相同时按轴号字典序）。
- 轴号允许是任意字符串，支持附加轴 `"1/1"`、`"2A"`。
- `x` 和 `y` 至少给一组；某一组为空时该方向退化成「直接输出毫米坐标」。
- 不需要每个构件都落在某个轴上 —— 算法取**最近轴 + 四舍五入偏移**，
  落在轴外会写成 `<11+500,D+3554>` 这样的形式。

## 比对两份计算书（compare）

转换出来的计算书和一份「标准答案」对不对得上，用 `compare` 子命令比。

```bash
# 方式一：三个位置参数
./zhiduoxing2xls compare 新工程计算书.xls 新.xls 测试结果.json

# 方式二：显式参数
./zhiduoxing2xls compare -gen 计算书.xls -ref 新.xls -json 结果.json -out 比对.xls -y
```

**比对键是「楼层 + 构件名 + 图元位置」**。同一个坐标、同一个构件下，
把两侧的「工程量计算式」各自收成一个集合，再比集合：

| 情况 | 结果列取值 |
| --- | --- |
| 两边都有，且算式集合相同（行序不同不算差异） | 一致 |
| 两边都有，但算式集合不同 | 不一致 |
| 只有计算书里有这个坐标 | 仅计算书有 |
| 只有参照文件里有这个坐标 | 仅参考文件有 |

输出是 8 列 `.xls`：

| 序号 | 楼层 | 构件类型 | 构件名称 | 图元位置 | 工程量计算式（计算书） | 工程量计算式（参考文件） | 比对结果 |
| --- | --- | --- | --- | --- | --- | --- | --- |

**楼层名一律按测试结果 JSON 输出**（`LCH=1` → 首层，`LCH=2` → 二层），
不取计算书里写的楼层名，也不会因为两份文件的楼层写法不同而错位；
要临时覆盖可以用 `-floor`。

```go
rep, err := zhiduoxingxls.Compare(zhiduoxingxls.CompareOptions{
	Generated:  "新工程计算书.xls",
	Reference:  "新.xls",
	ResultJSON: "测试结果.json",
	Overwrite:  true,
})
if err != nil {
	return err
}
log.Printf("一致 %d、不一致 %d、仅计算书 %d、仅参考 %d → %s",
	rep.Same, rep.Diff, rep.OnlyGenerated, rep.OnlyReference, rep.Out)
```

> ⚠️ 两份计算书必须来自**同一个工程**才有比对意义。不同工程的轴网与构件命名都不同
> （老工程写 `<6,D>` / `KZ9`，新工程写 `<1/1-1000,D+100>` / `KZ-9`），
> 比对结果会是满屏「仅计算书有 / 仅参考文件有」——这不是 bug，是两份数据本来就没有交集。

## 版式还原要点

| 项目 | 取值 |
| --- | --- |
| 工作表名 | `绘图输入构件工程量计算书-首层` |
| 列宽 | `1865 / 5522 / 17518`（另有一列 17518 作为收尾记录） |
| 默认列宽 / 行高 | `8` 字符 / `255` twips |
| 字体 | 单元格统一宋体 9 磅；表头、构件标题行、计算式首行的构件名加粗 |
| 行高 | 标题行与「柱: xx」行固定 `285`；明细行 `285 + (显示行数-1) × 210` |
| 自动换行容量 | 79 个半角宽度（CJK 记 2，其余记 1） |
| 边框 | 左右为细线，表格最上沿与最下沿为中粗线；计算式首行/明细行共用一条边（上下框线互斥） |

版式对照结果（生成文件 vs `新.xls`）：

```
工作表名一致        : True
字体表一致          : True
默认列宽 / 行高      : 8 / 255   （模板 8 / 255）
列宽一致            : True
单元格 XF 样式       : 11/11 全部一致（字体字重、对齐、自动换行、四边框线）
行高公式            : 625/625 命中
```

## 代码结构

```
zhiduoxingxls.go      对外公开包：Convert / ConvertBytes / Options / Report（你的项目 import 这个）
compare.go            对外公开包：Compare / CompareOptions / CompareReport（计算书比对）
cmd/zhiduoxing2xls/   CLI 入口（公开包的薄封装，含 compare 子命令）
internal/safejson/    容错 JSON 解析（BOM/注释/尾随逗号/围栏/单引号/片段抽取）
internal/model/       JSON 数据模型（字符串与数字均可容忍）
internal/spec/        「构件子类 / 工程量编码」→ 显示名、原始量标签、单位、顺序的规则表
internal/render/      数值规范化（big.Rat 精确四舍五入）、计算式渲染、行高估算
internal/build/       表格装配：分组、排序、合并区、行高、样式
internal/compare/     计算书解析（三列版式 → 图元行）+ 按坐标归并 + 比对结果表
internal/xls/         BIFF8 记录层 + OLE2 容器读写（零依赖；读端支持迷你流）
internal/grid/        轴网模型 + 「平面坐标 → 轴号写法」换算
internal/geom/        三维模型 JSON → 图元特征点（含门窗洞口偏移、多跨梁收敛、过梁补足）
```

`internal/` 下的包按 Go 的可见性规则不允许模块外直接 import，公开包是唯一的对外面；
这样后续重构内部实现不会破坏调用方。

## 已收录的构件类型

柱、构造柱、砌体墙、混凝土墙、门、窗、墙洞、过梁、梁、圈梁、现浇板、楼梯、
地沟、建筑面积、散水、台阶、栏杆扶手。

遇到未收录的子类会按「构件」兜底输出，并在统计里告警，可用 `-mapping` 覆盖修正。
`GJ_B_BFJ`（板负筋）、`GJ_B_BSLJ`（板受力筋）在参照文件里没有对应量名，默认跳过并计入统计。

## 测试

```bash
go test ./...
```

覆盖内容包括：容错解析的各种脏数据形态、数值规范化与量纲换算、行高公式、
表格装配（合并区 / 重复填值 / 末行收口样式），以及几条最容易踩坑的约束：

- 公开包行为：默认输出路径推导、`Overwrite` 保护、`ConvertBytes` 不落盘、
  输出确实是 OLE2 容器（`D0 CF 11 E0`）、`positions.csv` 自动读取、`OmitQuantity` 生效。
- `TestCFBStreamChainLength`：OLE2 里流的 FAT 链长度必须**恰好**等于 `ceil(流字节数/512)`。
  多分配扇区（例如把流对齐到 4096）会让 xlrd 报
  `size exceeds expected N bytes; corrupt?`，Excel 也可能拒绝打开。
- `TestBiffGlobalsStructure`：BIFF8 的字体索引 4 是保留占位项，真实文件**不写出**该条
  FONT 记录，读取器会自动补位；自己多写一条会让后面所有字体索引整体错位 1 位。
  `WRITEACCESS` 也必须写成规范的 BIFF8 字符串（长度前缀 + 选项字节）。

`internal/grid` 与 `internal/geom` 另有坐标换算的用例：轴号取最近轴 + 四舍五入偏移、
门窗沿宿主墙偏移 `DKKD/2`、多跨梁只留首尾两点、过梁按 `CD` 补足、PID 优先 / CID 兜底。

## 位置列的正确性怎么验证

`新.xls` 与当前这份计算结果 JSON **不是同一次导出**（同一根 KL17(3)，前者按跨拆成
12.875 / 5.55 / 9.55 三段，后者是 18.075 + 9.050 两段），所以它只能当**版式**基准，
不能当位置真值。位置是靠下面两条自查的：

1. **长度对账**（不依赖模板）：把生成的 `<起点><终点>` 反算成毫米长度，
   与结果 JSON 计算式里同一构件的「中心线长度 / 长度 / 轴线长度」相比。
   变截面梁要把各段长度去重后相加才是总长。
   实测：259 个输出图元中 110 个线式构件**全部**吻合（≤0.5%），
   其余 149 个是柱 / 门 / 窗 / 板等点式构件，没有可比的长度。
2. **与 `新.xls` 逐组比集合**：85 个共有分组里，
   柱 / 构造柱 / 砌体墙 / 窗 / 现浇板的位置集合**完全一致**（个别组只是组内顺序不同），
   与模板不同源的梁 / 圈梁 / 过梁 / 门则因两版几何不同而必然有差异。

> 复核脚本见 `work/validate.py`（长度对账）与 `work/compare.py`（逐组比集合）。

## 已知限制

- 「图元位置」列默认留空：源 JSON 不含定位信息（`Shapes` 为空、无轴网/坐标字段），
  需传 `-model` / `Options.ModelPath`、或 `Options.Positions`、或放一份 `positions.csv`。
- 轴网内置为智多星首层（1..11 / A..E）。**换工程必须换轴网**：把智多星导出的轴网
  JSON 用 `-axes` / `Options.AxesPath` 传进来即可（原生格式与 `{x,y}` 简写都认），
  详见《换一套轴网》。
- 多跨构件（多跨梁、转角墙）只写首尾两点。计算结果里它们是一个图元、只有一个单元格，
  而轴网写法最多容纳两组 `<>`，折点信息无法保留。
- 两个文件必须来自**同一次导出**。按 PID 对齐时若模型是别的版本，
  图元数或 PID 对不上，程序会在统计里提示「模型文件里没有能与计算结果对上的图元坐标」。
- `GJ_B_BFJ`（板负筋）、`GJ_B_BSLJ`（板受力筋）在参照文件里没有对应量名，默认跳过并计入统计。
- 88 条 `GCL_JSJMJ` 属于类型规则之外，默认不输出，可用 `-append-extra` / `Options.AppendExtra` 打开。
- 只生成单张工作表；多楼层需按楼层分别调用。
