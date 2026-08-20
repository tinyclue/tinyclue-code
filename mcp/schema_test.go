package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestToParameters 验证原始 JSON Schema → apitypes.Parameters 的映射，
// 覆盖嵌套 properties/required/items/enum/default/description。
func TestToParameters(t *testing.T) {
	raw := map[string]any{
		"type":     "object",
		"required": []any{"name", "tags"},
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "the name",
			},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"meta": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"size": map[string]any{"type": "integer", "default": 1.0},
				},
				"required": []any{"size"},
			},
			"level": map[string]any{
				"type": "string",
				"enum": []any{"low", "high"},
			},
		},
	}

	p := toParameters(raw)
	if p.Type != "object" {
		t.Fatalf("Type = %q, want object", p.Type)
	}
	if len(p.Required) != 2 || p.Required[0] != "name" || p.Required[1] != "tags" {
		t.Fatalf("Required = %v, want [name tags]", p.Required)
	}

	name, ok := p.Properties["name"]
	if !ok || name.Type != "string" || name.Description != "the name" {
		t.Fatalf("name item = %+v", name)
	}

	tags := p.Properties["tags"]
	if tags.Type != "array" || tags.Items == nil || tags.Items.Type != "string" {
		t.Fatalf("tags item = %+v", tags)
	}

	meta := p.Properties["meta"]
	if meta.Type != "object" {
		t.Fatalf("meta = %+v", meta)
	}
	size := meta.Properties["size"]
	if size.Type != "integer" || size.Default != 1.0 {
		t.Fatalf("size = %+v", size)
	}
	if len(meta.Required) != 1 || meta.Required[0] != "size" {
		t.Fatalf("meta.Required = %v", meta.Required)
	}

	level := p.Properties["level"]
	if len(level.Enum) != 2 || level.Enum[0] != "low" || level.Enum[1] != "high" {
		t.Fatalf("level = %+v", level)
	}
}

// TestToParametersDegrades 验证异常输入宽松降级（稳定性优先），绝不 panic。
func TestToParametersDegrades(t *testing.T) {
	if p := toParameters(nil); p.Type != "object" {
		t.Fatalf("nil input = %+v", p)
	}
	if p := toParameters(map[string]any{"properties": "not-a-map"}); p.Type != "object" {
		t.Fatalf("bad properties = %+v", p)
	}
	p := toParameters(map[string]any{"properties": map[string]any{"x": "not-an-object"}})
	if p.Type != "object" {
		t.Fatalf("bad item = %+v", p)
	}
	if v, ok := p.Properties["x"]; !ok || v.Type != "object" {
		t.Fatalf("bad item should degrade to {type:object}, got %+v", v)
	}
	p = toParameters(map[string]any{"properties": map[string]any{"x": map[string]any{"type": 123}}})
	if p.Type != "object" {
		t.Fatalf("bad type = %+v", p)
	}
}

// TestToParametersRequiredNonNil 回归：无 required 键的 schema 必须产生空数组而非 nil，
// 否则 Parameters.Required（无 omitempty）序列化为 "required": null，被模型 API 拒绝。
func TestToParametersRequiredNonNil(t *testing.T) {
	p := toParameters(map[string]any{"type": "object", "properties": map[string]any{}})
	if p.Required == nil {
		t.Fatalf("Required = nil, want non-nil empty slice")
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(data); !strings.Contains(got, `"required":[]`) {
		t.Fatalf("marshaled = %s, want required serialized as []", got)
	}
}
