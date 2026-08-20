package providers

import (
	"strings"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type AmazonBedrockProvider struct {
	ProviderSkeleton
}

func (p *AmazonBedrockProvider) Name() string { return types.ProviderNameAmazonBedrock }

func (p *AmazonBedrockProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.AmazonBedrock
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		if strings.HasPrefix(id, "ai21.jamba") {
			continue
		}
		if strings.HasPrefix(id, "mistral.mistral-7b-instruct-v0") {
			continue
		}

		baseURL := "https://bedrock-runtime.us-east-1.amazonaws.com"
		if strings.HasPrefix(id, "eu.") {
			baseURL = "https://bedrock-runtime.eu-central-1.amazonaws.com"
		}

		model := BuildModel(id, m, "bedrock-converse-stream", types.ProviderNameAmazonBedrock, baseURL)
		result = append(result, model)
	}
	return result
}

func (p *AmazonBedrockProvider) Patch(models []types.Model) []types.Model {
	for i, m := range models {
		// Phase 3: Fix cache pricing for Bedrock Opus 4.6 v1.
		if m.ID == "anthropic.claude-opus-4-6-v1" {
			models[i].Cost.CacheRead = 0.5
			models[i].Cost.CacheWrite = 6.25
		}
	}
	return models
}

func (p *AmazonBedrockProvider) Inject() []types.Model {
	return []types.Model{
		{
			ID:            "eu.anthropic.claude-opus-4-6-v1",
			Name:          "Claude Opus 4.6 (EU)",
			API:           "bedrock-converse-stream",
			BaseURL:       "https://bedrock-runtime.eu-central-1.amazonaws.com",
			Provider:      types.ProviderNameAmazonBedrock,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 200000,
			MaxTokens:     128000,
			Cost: types.ModelCost{
				Input:      5,
				Output:     25,
				CacheRead:  0.5,
				CacheWrite: 6.25,
			},
		},
	}
}
