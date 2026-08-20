package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type AnthropicProvider struct {
	ProviderSkeleton
}

func (p *AnthropicProvider) Name() string { return types.ProviderNameAnthropic }

func (p *AnthropicProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.Anthropic
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "anthropic-messages", types.ProviderNameAnthropic, "https://api.anthropic.com")
		result = append(result, model)
	}
	return result
}

func (p *AnthropicProvider) Patch(models []types.Model) []types.Model {
	for i, m := range models {
		if m.ID == "claude-opus-4-5" {
			models[i].Cost.CacheRead = 0.5
			models[i].Cost.CacheWrite = 6.25
		}
		if m.ID == "claude-opus-4-6" || m.ID == "claude-sonnet-4-6" ||
			m.ID == "claude-opus-4.6" || m.ID == "claude-sonnet-4.6" {
			models[i].ContextWindow = 1000000
		}
	}
	return models
}

func (p *AnthropicProvider) Inject() []types.Model {
	return []types.Model{
		{
			ID:            "claude-opus-4-6",
			Name:          "Claude Opus 4.6",
			API:           "anthropic-messages",
			BaseURL:       "https://api.anthropic.com",
			Provider:      types.ProviderNameAnthropic,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 1000000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input:      5,
				Output:     25,
				CacheRead:  0.5,
				CacheWrite: 6.25,
			},
		},
		{
			ID:            "claude-opus-4-7",
			Name:          "Claude Opus 4.7",
			API:           "anthropic-messages",
			BaseURL:       "https://api.anthropic.com",
			Provider:      types.ProviderNameAnthropic,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 1000000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input:      5,
				Output:     25,
				CacheRead:  0.5,
				CacheWrite: 6.25,
			},
		},
		{
			ID:            "claude-opus-4-8",
			Name:          "Claude Opus 4.8",
			API:           "anthropic-messages",
			BaseURL:       "https://api.anthropic.com",
			Provider:      types.ProviderNameAnthropic,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 1000000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input:      5,
				Output:     25,
				CacheRead:  0.5,
				CacheWrite: 6.25,
			},
		},
		{
			ID:            "claude-sonnet-4-6",
			Name:          "Claude Sonnet 4.6",
			API:           "anthropic-messages",
			BaseURL:       "https://api.anthropic.com",
			Provider:      types.ProviderNameAnthropic,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 1000000,
			MaxTokens:     64000,
			Cost: types.ModelCost{
				Input:      3,
				Output:     15,
				CacheRead:  0.3,
				CacheWrite: 3.75,
			},
		},
	}
}
