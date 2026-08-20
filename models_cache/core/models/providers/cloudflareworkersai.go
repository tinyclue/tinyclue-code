package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type CloudflareWorkersAIProvider struct {
	ProviderSkeleton
}

func (p *CloudflareWorkersAIProvider) Name() string { return types.ProviderNameCloudflareWorkersAI }

func (p *CloudflareWorkersAIProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.CloudflareWorkersAI
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "openai-completions", types.ProviderNameCloudflareWorkersAI,
			"https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1")
		model.Compat = map[string]any{"sendSessionAffinityHeaders": true}
		result = append(result, model)
	}
	return result
}

func (p *CloudflareWorkersAIProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *CloudflareWorkersAIProvider) Inject() []types.Model {
	return nil
}
