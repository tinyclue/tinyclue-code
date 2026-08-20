package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type DeepSeekProvider struct {
	ProviderSkeleton
}

func (p *DeepSeekProvider) Name() string { return types.ProviderNameDeepSeek }

func (p *DeepSeekProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *DeepSeekProvider) Inject() []types.Model {
	compat := map[string]any{
		"requiresReasoningContentOnAssistantMessages": true,
		"thinkingFormat": "deepseek",
	}
	return []types.Model{
		{
			ID:   "deepseek-v4-flash",
			Name: "DeepSeek V4 Flash",
			//API:  "anthropic-messages",
			//BaseURL:       "https://api.deepseek.com/anthropic",
			//Provider:      types.ProviderNameDeepSeek,
			API:           "openai-responses",
			BaseURL:       "https://api.openai.com/v1",
			Provider:      types.ProviderNameOpenAI,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 1000000,
			MaxTokens:     384000,
			Compat:        compat,
			Cost: types.ModelCost{
				Input:      0.14,
				Output:     0.28,
				CacheRead:  0.0028,
				CacheWrite: 0,
			},
		},
		{
			ID:   "deepseek-v4-pro",
			Name: "DeepSeek V4 Pro",
			//API:           "anthropic-messages",
			//BaseURL:       "https://api.deepseek.com/anthropic",
			//Provider:      types.ProviderNameDeepSeek,
			API:           "openai-responses",
			BaseURL:       "https://api.openai.com/v1",
			Provider:      types.ProviderNameDeepSeek,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 1000000,
			MaxTokens:     384000,
			Compat:        compat,
			Cost: types.ModelCost{
				Input:      0.435,
				Output:     0.87,
				CacheRead:  0.003625,
				CacheWrite: 0,
			},
		},
	}
}
