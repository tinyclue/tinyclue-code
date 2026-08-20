package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type MistralProvider struct {
	ProviderSkeleton
}

func (p *MistralProvider) Name() string { return types.ProviderNameMistral }

func (p *MistralProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.Mistral
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "mistral-conversations", types.ProviderNameMistral, "https://api.mistral.ai")
		result = append(result, model)
	}
	return result
}

func (p *MistralProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *MistralProvider) Inject() []types.Model {
	return []types.Model{
		{
			ID:            "mistral-medium-3.5",
			Name:          "Mistral Medium 3.5",
			API:           "mistral-conversations",
			BaseURL:       "https://api.mistral.ai",
			Provider:      types.ProviderNameMistral,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 262144,
			MaxTokens:     262144,
			Cost: types.ModelCost{
				Input:      1.5,
				Output:     7.5,
				CacheRead:  0,
				CacheWrite: 0,
			},
		},
	}
}
