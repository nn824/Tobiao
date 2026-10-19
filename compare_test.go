package zhiduoxingxls_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zhiduoxingxls "github.com/ninglang/zhiduoxing-xls"
	"github.com/ninglang/zhiduoxing-xls/internal/xls"
)

// mustConvertWithPosition 生成一份带「图元位置」的计算书，供比对测试使用。
func mustConvertWithPosition(t *testing.T) string {
	t.Helper()
	in := writeSample(t, sampleJSON)
	return mustConvert(t, in, zhiduoxingxls.Options{
		Overwrite: true,
		Positions: map[string]string{"c1": "<6,D>", "c2": "<7,D>"},
	})
}

// TestCompareSelfAllSame：同一个文件跟它自己比，必须 100% 一致。
// 这条断言同时验证了写 → 读 → 解析 → 归并的整条链路。
func TestCompareSelfAllSame(t *testing.T) {
	in := writeSample(t, sampleJSON)
	book := mustConvert(t, in, zhiduoxingxls.Options{
		Overwrite: true,
		Positions: map[string]string{"c1": "<6,D>", "c2": "<7,D>"},
	})

	rep, err := zhiduoxingxls.Compare(zhiduoxingxls.CompareOptions{
		Generated:  book,
		Reference:  book,
		ResultJSON: in,
		Overwrite:  true,
	})
	if err != nil {
		t.Fatalf("Compare 失败: %v", err)
	}
	if rep.GeneratedRows != 2 || rep.ReferenceRows != 2 {
		t.Fatalf("两侧都应解析出 2 个图元，实际 %d / %d", rep.GeneratedRows, rep.ReferenceRows)
	}
	if rep.Items != 2 {
		t.Fatalf("应有 2 个比对键，实际 %d", rep.Items)
	}
	if rep.Same != 2 || rep.Diff != 0 || rep.OnlyGenerated != 0 || rep.OnlyReference != 0 {
		t.Fatalf("自己比自己应全部一致，实际 %+v", rep)
	}
	if rep.Floor != "首层" {
		t.Errorf("楼层名应按测试结果 JSON 输出「首层」，实际 %q", rep.Floor)
	}

	// 默认输出路径：与计算书同目录的「<名字>-比对.xls」。
	want := filepath.Join(filepath.Dir(book),
		strings.TrimSuffix(filepath.Base(book), filepath.Ext(book))+"-比对.xls")
	if rep.Out != want {
		t.Errorf("默认输出路径应为 %s，实际 %s", want, rep.Out)
	}

	data, err := os.ReadFile(rep.Out)
	if err != nil {
		t.Fatalf("读输出文件失败: %v", err)
	}
	if !bytes.HasPrefix(data, oleMagic) {
		t.Fatal("输出不是 OLE2 复合文档（.xls）")
	}

	// 读回来检查版式：8 列、表头、4 个合计行（表头 + 2 条比对）。
	b, err := xls.ReadFile(rep.Out)
	if err != nil {
		t.Fatalf("读回比对结果失败: %v", err)
	}
	sheet := b.Sheets[0]
	if sheet.Name != "工程量计算式比对" {
		t.Errorf("工作表名应为「工程量计算式比对」，实际 %q", sheet.Name)
	}
	if len(sheet.Rows) != 3 {
		t.Errorf("应为表头 + 2 行，实际 %d 行", len(sheet.Rows))
	}
	head := []string{"序号", "楼层", "构件类型", "构件名称", "图元位置",
		"工程量计算式（计算书）", "工程量计算式（参考文件）", "比对结果"}
	for c, h := range head {
		cell, ok := sheet.CellAt(0, c)
		if !ok || cell.Value() != h {
			t.Errorf("表头第 %d 列应为 %q，实际 %q", c, h, cell.Value())
		}
	}
	if cell, _ := sheet.CellAt(1, 1); cell.Value() != "首层" {
		t.Errorf("楼层列应为「首层」，实际 %q", cell.Value())
	}
	if cell, _ := sheet.CellAt(1, 7); cell.Value() != "一致" {
		t.Errorf("比对结果列应为「一致」，实际 %q", cell.Value())
	}
}

// TestCompareOverwriteGuard 保证不会误删已有结果文件。
func TestCompareOverwriteGuard(t *testing.T) {
	in := writeSample(t, sampleJSON)
	book := mustConvertWithPosition(t)
	out := filepath.Join(filepath.Dir(book), "比对.xls")

	if err := os.WriteFile(out, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := zhiduoxingxls.Compare(zhiduoxingxls.CompareOptions{
		Generated:  book,
		Reference:  book,
		ResultJSON: in,
		Out:        out,
	})
	if !errors.Is(err, zhiduoxingxls.ErrOutputExists) {
		t.Fatalf("已存在输出文件时应返回 ErrOutputExists，实际 %v", err)
	}
	if data, _ := os.ReadFile(out); string(data) != "keep me" {
		t.Error("被拒绝时不应改动已有文件")
	}
}

// TestCompareMissingArgs 覆盖必填入参校验。
func TestCompareMissingArgs(t *testing.T) {
	if _, err := zhiduoxingxls.Compare(zhiduoxingxls.CompareOptions{Reference: "a.xls"}); err == nil {
		t.Error("缺少 Generated 时应报错")
	}
	if _, err := zhiduoxingxls.Compare(zhiduoxingxls.CompareOptions{Generated: "a.xls"}); err == nil {
		t.Error("缺少 Reference 时应报错")
	}
}

// TestCompareFloorFromJSON 验证楼层名确实来自测试结果 JSON 而不是计算书。
func TestCompareFloorFromJSON(t *testing.T) {
	// 楼层号 3 → 「三层」，计算书里写的也是「三层」，但这里换成 JSON 的楼层号 2。
	in := writeSample(t, strings.Replace(sampleJSON, `"LCH": 1`, `"LCH": 2`, 1))
	book := mustConvertWithPosition(t)

	rep, err := zhiduoxingxls.Compare(zhiduoxingxls.CompareOptions{
		Generated:  book,
		Reference:  book,
		ResultJSON: in,
		Overwrite:  true,
	})
	if err != nil {
		t.Fatalf("Compare 失败: %v", err)
	}
	if rep.Floor != "二层" {
		t.Fatalf("楼层名应取 JSON 的「二层」，实际 %q", rep.Floor)
	}
}
