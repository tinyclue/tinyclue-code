package providers

import (
	"strings"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type XiaomiProvider struct {
	ProviderSkeleton
}

func (p *XiaomiProvider) Name() string { return types.ProviderNameXiaomi }

func (p *XiaomiProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.Xiaomi
	if pb == nil {
		return nil
	}

	variants := []struct {
		provider string
		baseURL  string
	}{
		{types.ProviderNameXiaomi, "https://api.xiaomimimo.com/v1"},
		{"xiaomi-token-plan-cn", "https://token-plan-cn.xiaomimimo.com/v1"},
		{"xiaomi-token-plan-ams", "https://token-plan-ams.xiaomimimo.com/v1"},
		{"xiaomi-token-plan-sgp", "https://token-plan-sgp.xiaomimimo.com/v1"},
	}

	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}

		for _, v := range variants {
			if strings.HasPrefix(v.provider, "xiaomi-token-plan-") && id == "mimo-v2-flash" {
				continue
			}

			model := BuildModel(id, m, "openai-completions", v.provider, v.baseURL)
			model.Compat = map[string]any{
				"requiresReasoningContentOnAssistantMessages": true,
				"thinkingFormat": "deepseek",
			}
			result = append(result, model)
		}
	}
	return result
}

func (p *XiaomiProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *XiaomiProvider) Inject() []types.Model {
	return nil
}
