package mcp

import (
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

// toParameters 将 MCP 工具的 inputSchema（JSON Schema，来自 tools/list）转换为
// apitypes.Parameters。未知/复杂键宽松降级为 {type:"object"}，绝不失败（稳定性优先）。
func toParameters(raw map[string]any) apitypes.Parameters {
	// Required 必须初始化为空数组而非 nil：Parameters.Required 无 omitempty，
	// nil 会序列化成 "required": null，被模型 API 拒绝（null is not of type array）。
	p := apitypes.Parameters{Type: "object", Required: []string{}, Properties: map[string]apitypes.SchemaItem{}}
	if raw == nil {
		return p
	}
	if req, ok := raw["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				p.Required = append(p.Required, s)
			}
		}
	}
	if props, ok := raw["properties"].(map[string]any); ok {
		for name, v := range props {
			p.Properties[name] = toSchemaItem(v)
		}
	}
	return p
}

// toSchemaItem 递归转换 JSON Schema 节点为 apitypes.SchemaItem。
func toSchemaItem(v any) apitypes.SchemaItem {
	m, ok := v.(map[string]any)
	if !ok {
		return apitypes.SchemaItem{Type: "object"}
	}
	item := apitypes.SchemaItem{Type: "object"}
	if t, ok := m["type"].(string); ok {
		item.Type = t
	}
	if d, ok := m["description"].(string); ok {
		item.Description = d
	}
	if e, ok := m["enum"].([]any); ok {
		item.Enum = e
	}
	if d, ok := m["default"]; ok {
		item.Default = d
	}
	if it, ok := m["items"]; ok {
		if nested, ok := it.(map[string]any); ok {
			s := toSchemaItem(nested)
			item.Items = &s
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		item.Properties = map[string]apitypes.SchemaItem{}
		for name, pv := range props {
			item.Properties[name] = toSchemaItem(pv)
		}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				item.Required = append(item.Required, s)
			}
		}
	}
	return item
}
