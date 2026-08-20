package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type MoonshotAICNProvider struct {
	ProviderSkeleton
}

func (p *MoonshotAICNProvider) Name() string { return types.ProviderNameMoonshotAICN }

func (p *MoonshotAICNProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.MoonshotAICN
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "openai-completions", types.ProviderNameMoonshotAICN,
			"https://api.moonshot.cn/v1")
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

func (p *MoonshotAICNProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *MoonshotAICNProvider) Inject() []types.Model {
	return nil
}
