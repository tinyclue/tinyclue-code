package providers

import "github.com/tinyclue/tinyclue-code/models_cache/core/types"

// BuildModel constructs a Model from a ModelsDevModel entry.
func BuildModel(id string, m types.ModelsDevModel, api, provider, baseURL string) types.Model {
	name := m.Name
	if name == "" {
		name = id
	}

	reasoning := false
	if m.Reasoning != nil {
		reasoning = *m.Reasoning
	}

	input := []string{"text"}
	if m.Modalities != nil && Contains(m.Modalities.Input, "image") {
		input = append(input, "image")
	}

	cost := types.ModelCost{}
	if m.Cost != nil {
		if m.Cost.Input != nil {
			cost.Input = *m.Cost.Input
		}
		if m.Cost.Output != nil {
			cost.Output = *m.Cost.Output
		}
		if m.Cost.CacheRead != nil {
			cost.CacheRead = *m.Cost.CacheRead
		}
		if m.Cost.CacheWrite != nil {
			cost.CacheWrite = *m.Cost.CacheWrite
		}
	}

	ctxWindow := 4096
	if m.Limit != nil && m.Limit.Context != nil {
		ctxWindow = *m.Limit.Context
	}

	maxTokens := 4096
	if m.Limit != nil && m.Limit.Output != nil {
		maxTokens = *m.Limit.Output
	}

	return types.Model{
		ID:            id,
		Name:          name,
		API:           api,
		BaseURL:       baseURL,
		Provider:      provider,
		Reasoning:     reasoning,
		Input:         input,
		Cost:          cost,
		ContextWindow: ctxWindow,
		MaxTokens:     maxTokens,
	}
}

// Ptr helps creating string pointers for ThinkingLevelMap values.
func Ptr(s string) *string { return &s }

// MergeCompat merges compat key-value pairs into a Model's Compat map.
func MergeCompat(m *types.Model, vals map[string]any) {
	compat, ok := m.Compat.(map[string]any)
	if !ok {
		compat = make(map[string]any, len(vals))
		m.Compat = compat
	}
	for k, v := range vals {
		compat[k] = v
	}
}

// Contains checks if a string slice contains an item.
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
