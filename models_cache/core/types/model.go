package types

// Model is the unified model definition after provider-specific filtering.
type Model struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	API              string             `json:"api"`
	BaseURL          string             `json:"baseUrl"`
	Provider         string             `json:"provider"`
	Reasoning        bool               `json:"reasoning"`
	Input            []string           `json:"input"`
	Cost             ModelCost          `json:"cost"`
	ContextWindow    int                `json:"contextWindow"`
	MaxTokens        int                `json:"maxTokens"`
	Compat           any                `json:"compat,omitempty"`
	Headers          map[string]string  `json:"headers,omitempty"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap,omitempty"`
}

// ModelCost holds per-token pricing in $/million tokens.
type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}
