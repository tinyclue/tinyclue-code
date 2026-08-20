package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type OpenAICodexProvider struct {
	ProviderSkeleton
}

func (p *OpenAICodexProvider) Name() string { return types.ProviderNameOpenAICodex }

func (p *OpenAICodexProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *OpenAICodexProvider) Inject() []types.Model {
	return []types.Model{
		{
			ID:            "gpt-5.3-codex-spark",
			Name:          "GPT-5.3 Codex Spark",
			API:           "openai-codex-responses",
			BaseURL:       "https://chatgpt.com/backend-api",
			Provider:      types.ProviderNameOpenAICodex,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 128000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input: 1.75, Output: 14, CacheRead: 0.175, CacheWrite: 0,
			},
		},
		{
			ID:            "gpt-5.4",
			Name:          "GPT-5.4",
			API:           "openai-codex-responses",
			BaseURL:       "https://chatgpt.com/backend-api",
			Provider:      types.ProviderNameOpenAICodex,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 272000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input: 2.5, Output: 15, CacheRead: 0.25, CacheWrite: 0,
			},
		},
		{
			ID:            "gpt-5.4-mini",
			Name:          "GPT-5.4 mini",
			API:           "openai-codex-responses",
			BaseURL:       "https://chatgpt.com/backend-api",
			Provider:      types.ProviderNameOpenAICodex,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 272000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input: 0.75, Output: 4.5, CacheRead: 0.075, CacheWrite: 0,
			},
		},
		{
			ID:            "gpt-5.5",
			Name:          "GPT-5.5",
			API:           "openai-codex-responses",
			BaseURL:       "https://chatgpt.com/backend-api",
			Provider:      types.ProviderNameOpenAICodex,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 272000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input: 5, Output: 30, CacheRead: 0.5, CacheWrite: 0,
			},
		},
	}
}
