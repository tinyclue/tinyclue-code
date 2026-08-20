package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type CerebrasProvider struct {
	ProviderSkeleton
}

func (p *CerebrasProvider) Name() string { return types.ProviderNameCerebras }

func (p *CerebrasProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.Cerebras
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "openai-completions", types.ProviderNameCerebras, "https://api.cerebras.ai/v1")
		result = append(result, model)
	}
	return result
}

func (p *CerebrasProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *CerebrasProvider) Inject() []types.Model {
	return nil
}
