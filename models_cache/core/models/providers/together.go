package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

var reasoningOnly = map[string]bool{
	"deepseek-ai/DeepSeek-R1": true,
	"MiniMaxAI/MiniMax-M2.7":  true,
}

var reasoningEffort = map[string]bool{
	"openai/gpt-oss-20b":  true,
	"openai/gpt-oss-120b": true,
}

var toggleReasoningEffort = map[string]bool{
	"deepseek-ai/DeepSeek-V4-Pro": true,
}

type TogetherProvider struct {
	ProviderSkeleton
}

func (p *TogetherProvider) Name() string { return types.ProviderNameTogether }

func (p *TogetherProvider) Filter(ctx *types.PipelineContext) []types.Model {
	var result []types.Model
	for _, pb := range []*types.ProviderBlock{
		ctx.ModelsDevData.Together,
		ctx.ModelsDevData.Togetherai,
		ctx.ModelsDevData.TogetherAI,
	} {
		if pb == nil {
			continue
		}
		for id, m := range pb.Models {
			if m.ToolCall == nil || !*m.ToolCall {
				continue
			}
			if m.Status != nil && *m.Status == "deprecated" {
				continue
			}

			reasoning := m.Reasoning != nil && *m.Reasoning

			model := BuildModel(id, m, "openai-completions", types.ProviderNameTogether,
				"https://api.together.ai/v1")

			compat := map[string]any{
				"supportsStore":              false,
				"supportsDeveloperRole":      false,
				"supportsReasoningEffort":    false,
				"maxTokensField":             "max_tokens",
				"supportsStrictMode":         false,
				"supportsLongCacheRetention": false,
			}

			if reasoning {
				if reasoningEffort[id] {
					compat["supportsReasoningEffort"] = true
					compat["thinkingFormat"] = "openai"
				} else if toggleReasoningEffort[id] {
					compat["thinkingFormat"] = "together"
					compat["supportsReasoningEffort"] = true
				} else if !reasoningOnly[id] {
					compat["thinkingFormat"] = "together"
				}
			}
			model.Compat = compat

			if reasoning {
				levelMap := map[string]*string{}
				if reasoningEffort[id] {
					levelMap["off"] = nil
					levelMap["minimal"] = nil
				} else if toggleReasoningEffort[id] {
					levelMap["minimal"] = nil
					levelMap["low"] = nil
					levelMap["medium"] = nil
					levelMap["high"] = Ptr("high")
					levelMap["xhigh"] = nil
				} else if reasoningOnly[id] {
					levelMap["off"] = nil
					levelMap["minimal"] = nil
					levelMap["low"] = nil
					levelMap["medium"] = nil
				} else {
					levelMap["minimal"] = nil
					levelMap["low"] = nil
					levelMap["medium"] = nil
				}
				model.ThinkingLevelMap = levelMap
			}

			result = append(result, model)
		}
	}
	return result
}

func (p *TogetherProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *TogetherProvider) Inject() []types.Model {
	return nil
}
