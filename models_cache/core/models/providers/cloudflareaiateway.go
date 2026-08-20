package providers

import (
	"strings"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type CloudflareAIGatewayProvider struct {
	ProviderSkeleton
}

func (p *CloudflareAIGatewayProvider) Name() string { return types.ProviderNameCloudflareAIGateway }

func (p *CloudflareAIGatewayProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.CloudflareAIGateway
	if pb == nil {
		return nil
	}
	const (
		gatewayOpenAIBase    = "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"
		gatewayAnthropicBase = "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic"
		gatewayCompatBase    = "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/compat"
	)

	var result []types.Model
	for prefixedID, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}

		slashIdx := strings.Index(prefixedID, "/")
		if slashIdx == -1 {
			continue
		}
		upstream := prefixedID[:slashIdx]
		nativeID := prefixedID[slashIdx+1:]

		var api, baseURL, id string
		switch upstream {
		case "openai":
			api = "openai-responses"
			baseURL = gatewayOpenAIBase
			id = nativeID
		case "anthropic":
			api = "anthropic-messages"
			baseURL = gatewayAnthropicBase
			id = nativeID
		case "workers-ai":
			api = "openai-completions"
			baseURL = gatewayCompatBase
			id = prefixedID
		default:
			continue
		}

		model := BuildModel(id, m, api, types.ProviderNameCloudflareAIGateway, baseURL)
		if upstream == "workers-ai" {
			model.Compat = map[string]any{"sendSessionAffinityHeaders": true}
		}
		result = append(result, model)
	}
	return result
}

func (p *CloudflareAIGatewayProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *CloudflareAIGatewayProvider) Inject() []types.Model {
	return nil
}
