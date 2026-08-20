package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type GoogleProvider struct {
	ProviderSkeleton
}

func (p *GoogleProvider) Name() string { return types.ProviderNameGoogle }

func (p *GoogleProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.Google
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "google-generative-ai", types.ProviderNameGoogle, "https://generativelanguage.googleapis.com/v1beta")
		result = append(result, model)
	}
	return result
}

func (p *GoogleProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *GoogleProvider) Inject() []types.Model {
	return []types.Model{
		{
			ID:            "gemini-3.1-flash-lite-preview",
			Name:          "Gemini 3.1 Flash Lite Preview",
			API:           "google-generative-ai",
			BaseURL:       "https://generativelanguage.googleapis.com/v1beta",
			Provider:      types.ProviderNameGoogle,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 1048576,
			MaxTokens:     65536,
			Cost: types.ModelCost{
				Input:      0,
				Output:     0,
				CacheRead:  0,
				CacheWrite: 0,
			},
		},
	}
}
