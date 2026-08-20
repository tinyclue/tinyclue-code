package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type OpenAIProvider struct {
	ProviderSkeleton
}

func (p *OpenAIProvider) Name() string { return types.ProviderNameOpenAI }

func (p *OpenAIProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.OpenAI
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "openai-responses", types.ProviderNameOpenAI, "https://api.openai.com/v1")
		result = append(result, model)
	}
	return result
}

func (p *OpenAIProvider) Patch(models []types.Model) []types.Model {
	for i, m := range models {
		// Phase 4: Context window overrides for gpt-5.4 and gpt-5.5.
		if m.ID == "gpt-5.4" || m.ID == "gpt-5.5" {
			models[i].ContextWindow = 272000
			models[i].MaxTokens = 128000
		}
		if m.ID == "gpt-5-pro" {
			models[i].MaxTokens = 128000
		}
	}
	return models
}

func (p *OpenAIProvider) Inject() []types.Model {
	return []types.Model{
		{
			ID:            "gpt-5-chat-latest",
			Name:          "GPT-5 Chat Latest",
			API:           "openai-responses",
			BaseURL:       "https://api.openai.com/v1",
			Provider:      types.ProviderNameOpenAI,
			Reasoning:     false,
			Input:         []string{"text", "image"},
			ContextWindow: 128000,
			MaxTokens:     16384,
			Cost: types.ModelCost{
				Input:      1.25,
				Output:     10,
				CacheRead:  0.125,
				CacheWrite: 0,
			},
		},
		{
			ID:            "gpt-5.1-codex",
			Name:          "GPT-5.1 Codex",
			API:           "openai-responses",
			BaseURL:       "https://api.openai.com/v1",
			Provider:      types.ProviderNameOpenAI,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 400000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input:      1.25,
				Output:     5,
				CacheRead:  0.125,
				CacheWrite: 1.25,
			},
		},
		{
			ID:            "gpt-5.1-codex-max",
			Name:          "GPT-5.1 Codex Max",
			API:           "openai-responses",
			BaseURL:       "https://api.openai.com/v1",
			Provider:      types.ProviderNameOpenAI,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 400000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input:      1.25,
				Output:     10,
				CacheRead:  0.125,
				CacheWrite: 0,
			},
		},
		{
			ID:            "gpt-5.3-codex-spark",
			Name:          "GPT-5.3 Codex Spark",
			API:           "openai-responses",
			BaseURL:       "https://api.openai.com/v1",
			Provider:      types.ProviderNameOpenAI,
			Reasoning:     true,
			Input:         []string{"text"},
			ContextWindow: 128000,
			MaxTokens:     16384,
			Cost: types.ModelCost{
				Input:      0,
				Output:     0,
				CacheRead:  0,
				CacheWrite: 0,
			},
		},
		{
			ID:            "gpt-5.4",
			Name:          "GPT-5.4",
			API:           "openai-responses",
			BaseURL:       "https://api.openai.com/v1",
			Provider:      types.ProviderNameOpenAI,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 272000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input:      2.5,
				Output:     15,
				CacheRead:  0.25,
				CacheWrite: 0,
			},
		},
	}
}
