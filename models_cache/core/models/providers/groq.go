package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type GroqProvider struct {
	ProviderSkeleton
}

func (p *GroqProvider) Name() string { return types.ProviderNameGroq }

func (p *GroqProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.Groq
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "openai-completions", types.ProviderNameGroq, "https://api.groq.com/openai/v1")
		result = append(result, model)
	}
	return result
}

func (p *GroqProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *GroqProvider) Inject() []types.Model {
	return nil
}
