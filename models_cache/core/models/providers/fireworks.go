package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type FireworksAIProvider struct {
	ProviderSkeleton
}

func (p *FireworksAIProvider) Name() string { return types.ProviderNameFireworks }

func (p *FireworksAIProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.FireworksAI
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "anthropic-messages", types.ProviderNameFireworks,
			"https://api.fireworks.ai/inference")
		model.Compat = map[string]any{
			"sendSessionAffinityHeaders":      true,
			"supportsEagerToolInputStreaming": false,
			"supportsCacheControlOnTools":     false,
			"supportsLongCacheRetention":      false,
		}
		result = append(result, model)
	}
	return result
}

func (p *FireworksAIProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *FireworksAIProvider) Inject() []types.Model {
	return nil
}
