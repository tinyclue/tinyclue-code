package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type HuggingFaceProvider struct {
	ProviderSkeleton
}

func (p *HuggingFaceProvider) Name() string { return types.ProviderNameHuggingFace }

func (p *HuggingFaceProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.HuggingFace
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "openai-completions", types.ProviderNameHuggingFace, "https://router.huggingface.co/v1")
		model.Compat = map[string]any{"supportsDeveloperRole": false}
		result = append(result, model)
	}
	return result
}

func (p *HuggingFaceProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *HuggingFaceProvider) Inject() []types.Model {
	return nil
}
