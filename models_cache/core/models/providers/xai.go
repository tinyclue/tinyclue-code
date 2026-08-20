package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type XAIProvider struct {
	ProviderSkeleton
}

func (p *XAIProvider) Name() string { return types.ProviderNameXAI }

func (p *XAIProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.XAI
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "openai-completions", types.ProviderNameXAI, "https://api.x.ai/v1")
		result = append(result, model)
	}
	return result
}

func (p *XAIProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *XAIProvider) Inject() []types.Model {
	return []types.Model{
		{
			ID:            "grok-3",
			Name:          "Grok 3",
			API:           "openai-completions",
			BaseURL:       "https://api.x.ai/v1",
			Provider:      types.ProviderNameXAI,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     8192,
			Cost: types.ModelCost{
				Input:      3,
				Output:     15,
				CacheRead:  0.75,
				CacheWrite: 0,
			},
		},
		{
			ID:            "grok-3-fast",
			Name:          "Grok 3 Fast",
			API:           "openai-completions",
			BaseURL:       "https://api.x.ai/v1",
			Provider:      types.ProviderNameXAI,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 131072,
			MaxTokens:     8192,
			Cost: types.ModelCost{
				Input:      5,
				Output:     25,
				CacheRead:  1.25,
				CacheWrite: 0,
			},
		},
		{
			ID:            "grok-code-fast-1",
			Name:          "Grok Code Fast 1",
			API:           "openai-completions",
			BaseURL:       "https://api.x.ai/v1",
			Provider:      types.ProviderNameXAI,
			Reasoning:     false,
			Input:         []string{"text"},
			ContextWindow: 32768,
			MaxTokens:     8192,
			Cost: types.ModelCost{
				Input:      0.2,
				Output:     1.5,
				CacheRead:  0.02,
				CacheWrite: 0,
			},
		},
	}
}
