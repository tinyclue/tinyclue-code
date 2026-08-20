package types

// SchemaItem 工具参数的单个属性，遵循 JSON Schema 规范。
type SchemaItem struct {
	Type        string                `json:"type"`
	Description string                `json:"description"`
	Enum        []any                 `json:"enum,omitempty"`
	Default     any                   `json:"default,omitempty"`
	Items       *SchemaItem           `json:"items,omitempty"`
	Properties  map[string]SchemaItem `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
}

// Parameters 工具参数的完整定义。
type Parameters struct {
	Type       string                `json:"type"`
	Properties map[string]SchemaItem `json:"properties"`
	Required   []string              `json:"required"`
}

// Tool 工具定义，用于请求中告知 AI 可用工具。
type Tool struct {
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Parameters     Parameters `json:"parameters"`
	Strict         bool       `json:"strict"`
	Type           string     `json:"type,omitempty"`            // 内置工具类型，如 WebSearchBuiltIn
	MaxUses        int        `json:"max_uses,omitempty"`        // 内置工具的最大调用次数
	AllowedDomains []string   `json:"allowed_domains,omitempty"` // web_search 允许的域名
	BlockedDomains []string   `json:"blocked_domains,omitempty"` // web_search 禁用的域名
}

const (
	// WebSearchBuiltIn 是 Anthropic Messages API 内置的 web search 工具类型。
	WebSearchBuiltIn = "web_search_20250305"
)
