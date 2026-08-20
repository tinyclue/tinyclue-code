package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type AntLingProvider struct {
	ProviderSkeleton
}

func (p *AntLingProvider) Name() string { return types.ProviderNameAntLing }

func (p *AntLingProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *AntLingProvider) Inject() []types.Model {
	baseCompat := map[string]any{
		"supportsStore":              false,
		"supportsDeveloperRole":      false,
		"supportsReasoningEffort":    false,
		"maxTokensField":             "max_tokens",
		"supportsLongCacheRetention": false,
	}
	ringCompat := map[string]any{
		"supportsStore":              false,
		"supportsDeveloperRole":      false,
		"supportsReasoningEffort":    false,
		"maxTokensField":             "max_tokens",
		"supportsLongCacheRetention": false,
		"thinkingFormat":             "ant-ling",
	}
	return []types.Model{
		{
			ID:            "Ling-2.6-flash",
			Name:          "Ling 2.6 Flash",
			API:           "openai-completions",
			BaseURL:       "https://api.ant-ling.com/v1",
			Provider:      types.ProviderNameAntLing,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 262144,
			MaxTokens:     65536,
			Compat:        baseCompat,
			Cost: types.ModelCost{
				Input: 0.01, Output: 0.02, CacheRead: 0, CacheWrite: 0,
			},
		},
		{
			ID:            "Ling-2.6-1T",
			Name:          "Ling 2.6 1T",
			API:           "openai-completions",
			BaseURL:       "https://api.ant-ling.com/v1",
			Provider:      types.ProviderNameAntLing,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 262144,
			MaxTokens:     65536,
			Compat:        baseCompat,
			Cost: types.ModelCost{
				Input: 0.06, Output: 0.25, CacheRead: 0, CacheWrite: 0,
			},
		},
		{
			ID:            "Ring-2.6-1T",
			Name:          "Ring 2.6 1T",
			API:           "openai-completions",
			BaseURL:       "https://api.ant-ling.com/v1",
			Provider:      types.ProviderNameAntLing,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 262144,
			MaxTokens:     65536,
			Compat:        ringCompat,
			Cost: types.ModelCost{
				Input: 0.06, Output: 0.25, CacheRead: 0, CacheWrite: 0,
			},
		},
	}
}
