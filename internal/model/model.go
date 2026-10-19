// Package model 定义智多星工程量计算结果 JSON 的数据结构。
package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Document 是导出 JSON 的根对象。
type Document struct {
	Shapes        map[string]json.RawMessage `json:"Shapes"`
	List          Section                    `json:"List"`
	Norm          Section                    `json:"Norm"`
	Rebar         json.RawMessage            `json:"Rebar"`
	ScheduleCount PlainString                `json:"ScheduleCount"`
}

// Section 是「绘图输入（List）」或「规范（Norm）」计算结果分组。
type Section struct {
	Null    []string          `json:"Null"`
	Error   map[string]string `json:"Error"`
	Cancel  json.RawMessage   `json:"Cancel"`
	Project Project           `json:"Project"`
}

// Project 是一个工程。
type Project struct {
	ID         string       `json:"ID"`
	SubProject []SubProject `json:"SubProject"`
}

// SubProject 是单位工程 / 分部。
type SubProject struct {
	ID    string  `json:"ID"`
	Floor []Floor `json:"Floor"`
}

// Floor 是楼层，LCH 为楼层号（1 表示首层）。
type Floor struct {
	ID         string      `json:"ID"`
	LCH        TolerantInt `json:"LCH"`
	Components []Component `json:"Components"`
}

// Component 是一个图元（构件实例）。
type Component struct {
	CID        string     `json:"CID"`
	PID        string     `json:"PID"`
	LX         string     `json:"LX"` // 构件大类编码，如 GJ_Z
	ZL         string     `json:"ZL"` // 构件子类编码，如 GJ_Z_TZ
	MC         string     `json:"MC"` // 构件名称，如 KZ9
	IsLook     bool       `json:"IsLook"`
	Quantities []Quantity `json:"Quantities"`
}

// Quantity 是一条工程量。
type Quantity struct {
	Type       string      `json:"@type"`
	LX         string      `json:"LX"` // 工程量编码，如 GCL_TJ
	YSL        *Quantity   `json:"YSL"`
	JSL        PlainString `json:"JSL"`   // 计算数量（整数，单位见单位表）
	JSS        PlainString `json:"JSS"`   // 计算式
	JSSJG      PlainString `json:"JSSJG"` // 计算式结果，形如 "=0.720m³"
	Deductions []Deduction `json:"Deductions"`
	Shape      PlainString `json:"Shape"`
}

// Deduction 是扣减项。
type Deduction struct {
	LX        string `json:"LX"`
	Component string `json:"Component"`
}

// PlainString 容忍字符串与数字两种表示形式，统一为字符串。
type PlainString string

// UnmarshalJSON 实现 json.Unmarshaler。
func (p *PlainString) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*p = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*p = PlainString(s)
		return nil
	}
	*p = PlainString(strings.TrimSpace(string(b)))
	return nil
}

// String 返回字符串值。
func (p PlainString) String() string { return string(p) }

// Int64 把数值字符串解析为 int64，非数值返回 0 与错误。
func (p PlainString) Int64() (int64, error) {
	s := strings.TrimSpace(string(p))
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// TolerantInt 容忍数字与字符串两种形式的整数。
type TolerantInt int

// UnmarshalJSON 实现 json.Unmarshaler。
func (t *TolerantInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*t = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("楼层号无法解析为整数: %q", s)
	}
	*t = TolerantInt(int(v))
	return nil
}

// Components 返回指定计算结果分组中所有楼层的图元，保持原始顺序。
func (s *Section) Components() []Component {
	var out []Component
	for _, sp := range s.Project.SubProject {
		for _, fl := range sp.Floor {
			out = append(out, fl.Components...)
		}
	}
	return out
}

// FirstFloor 返回第一个楼层，若不存在返回 nil。
func (s *Section) FirstFloor() *Floor {
	for i := range s.Project.SubProject {
		if len(s.Project.SubProject[i].Floor) > 0 {
			return &s.Project.SubProject[i].Floor[0]
		}
	}
	return nil
}

// ErrorCount 返回错误条目数量。
func (s *Section) ErrorCount() int { return len(s.Error) }

// Quantity 按编码返回工程量，第二个返回值表示是否存在。
func (c *Component) Quantity(code string) (Quantity, bool) {
	for _, q := range c.Quantities {
		if q.LX == code {
			return q, true
		}
	}
	return Quantity{}, false
}
