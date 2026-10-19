package safejson

import (
	"encoding/json"
	"testing"
)

type payload struct {
	Name  string `json:"name"`
	Items []int  `json:"items"`
	LCH   string `json:"lch"`
}

// Parse 的目标是「尽可能解析成功」，任何一种常见脏数据都不应该拖垮流程。
func TestParseTolerantForms(t *testing.T) {
	want := payload{Name: "首层", Items: []int{1, 2, 3}, LCH: "1"}

	cases := []struct {
		label string
		in    string
	}{
		{"标准 JSON", `{"name":"首层","items":[1,2,3],"lch":"1"}`},
		{"带 BOM", "\uFEFF" + `{"name":"首层","items":[1,2,3],"lch":"1"}`},
		{"尾随逗号", `{"name":"首层","items":[1,2,3,],"lch":"1",}`},
		{"行注释", "{\n// 注释\n\"name\":\"首层\",\"items\":[1,2,3],\n\"lch\":\"1\"\n}"},
		{"块注释", "{/* 注释 */\"name\":\"首层\",\"items\":[1,2,3],\"lch\":\"1\"}"},
		{"代码围栏", "```json\n{\"name\":\"首层\",\"items\":[1,2,3],\"lch\":\"1\"}\n```"},
		{"前后夹带说明", "以下是解析结果：\n{\"name\":\"首层\",\"items\":[1,2,3],\"lch\":\"1\"}\n解析完成。"},
		{"单引号", `{'name':'首层','items':[1,2,3],'lch':'1'}`},
	}
	for _, c := range cases {
		var got payload
		if err := Parse([]byte(c.in), &got); err != nil {
			t.Errorf("[%s] Parse 失败: %v", c.label, err)
			continue
		}
		if got.Name != want.Name || got.LCH != want.LCH || len(got.Items) != 3 {
			t.Errorf("[%s] 解析结果 = %+v，期望 %+v", c.label, got, want)
		}
	}
}

// 无法解析时必须返回错误而不是抛 panic，且错误信息要能定位问题。
func TestParseReturnsErrorNotPanic(t *testing.T) {
	var v payload
	if err := Parse([]byte("{ 这不是 JSON"), &v); err == nil {
		t.Fatal("垃圾输入应当返回错误")
	}
	if err := Parse(nil, &v); err == nil {
		t.Fatal("空输入应当返回错误")
	}
	if err := Parse([]byte(""), &v); err == nil {
		t.Fatal("空字符串应当返回错误")
	}
	// 合法 JSON 但类型不符，同样应是错误而非 panic。
	if err := Parse([]byte(`{"items":"不是数组"}`), &v); err == nil {
		t.Fatal("类型不符应当返回错误")
	}
	var out payload
	if err := Parse([]byte(`{"name":"x"}`), &out); err != nil {
		t.Fatalf("部分字段缺失不应报错: %v", err)
	}
	if out.Name != "x" || out.Items != nil {
		t.Fatalf("缺失字段应保持零值，得到 %+v", out)
	}
}

func TestExtractOuterMostObject(t *testing.T) {
	const src = `日志：开始
{"a":{"b":[1,2,{"c":"}"}]},"d":"]" }
日志：结束`
	got, ok := Extract(src)
	if !ok {
		t.Fatal("Extract 应当成功")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("抽出的片段不是合法 JSON: %v\n%s", err, got)
	}
	if _, ok := m["a"]; !ok {
		t.Fatalf("抽取结果缺少字段 a: %s", got)
	}
	if _, ok := Extract("没有任何 JSON"); ok {
		t.Fatal("无 JSON 时应返回 false")
	}
}

func TestRepairKeepsStringContent(t *testing.T) {
	// 字符串内部出现的 // 与 /* 不能被当作注释删掉，单引号也不应被改写。
	in := `{"url":"http://a/b","note":"/* 不是注释 */","q":"it's ok"}`
	out := Repair(in)
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("Repair 破坏了解析: %v\n%s", err, out)
	}
	if m["url"] != "http://a/b" || m["q"] != "it's ok" {
		t.Fatalf("字符串内容被改写: %+v", m)
	}
}

func TestValidUTF8(t *testing.T) {
	if !ValidUTF8([]byte("首层 - 柱 - KZ9")) {
		t.Error("合法 UTF-8 被判为非法")
	}
	if ValidUTF8([]byte{0xFF, 0xFE, 0x00}) {
		t.Error("非法 UTF-8 未被识别")
	}
}
