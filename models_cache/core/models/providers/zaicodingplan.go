package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

var unsupportedToolStream = map[string]bool{
	"glm-4.5":       true,
	"glm-4.5-air":   true,
	"glm-4.5-flash": true,
	"glm-4.5v":      true,
}

type ZAICodingPlanProvider struct {
	ProviderSkeleton
}

func (p *ZAICodingPlanProvider) Name() string { return types.ProviderNameZAICodingPlan }

func (p *ZAICodingPlanProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.ZAICodingPlan
	if pb == nil {
		return nil
	}

	variants := []struct {
		provider string
		baseURL  string
	}{
		{"zai", "https://api.z.ai/api/coding/paas/v4"},
		{"zai-coding-cn", "https://open.bigmodel.cn/api/coding/paas/v4"},
	}

	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}

		supportsImage := m.Modalities != nil && Contains(m.Modalities.Input, "image")

		for _, v := range variants {
			model := BuildModel(id, m, "openai-completions", v.provider, v.baseURL)
			model.Input = []string{"text"}
			if supportsImage {
				model.Input = append(model.Input, "image")
			}

			compat := map[string]any{
				"supportsDeveloperRole": false,
				"thinkingFormat":        "zai",
			}
			if !unsupportedToolStream[id] {
				compat["zaiToolStream"] = true
			}
			model.Compat = compat

			result = append(result, model)
		}
	}
	return result
}

func (p *ZAICodingPlanProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *ZAICodingPlanProvider) Inject() []types.Model {
	return nil
}
