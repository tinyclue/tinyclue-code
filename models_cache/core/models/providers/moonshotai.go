package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type MoonshotAIProvider struct {
	ProviderSkeleton
}

func (p *MoonshotAIProvider) Name() string { return types.ProviderNameMoonshotAI }

func (p *MoonshotAIProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.MoonshotAI
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "openai-completions", types.ProviderNameMoonshotAI,
			"https://api.moonshot.ai/v1")
		model.Compat = map[string]any{
			"supportsStore":           false,
			"supportsDeveloperRole":   false,
			"supportsReasoningEffort": false,
			"maxTokensField":          "max_tokens",
			"supportsStrictMode":      false,
			"thinkingFormat":          "deepseek",
		}
		result = append(result, model)
	}
	return result
}

func (p *MoonshotAIProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *MoonshotAIProvider) Inject() []types.Model {
	return nil
}
