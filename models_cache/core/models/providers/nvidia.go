package providers

import (
	"strings"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

var unsupportedModels = map[string]bool{
	"abacusai/dracarys-llama-3.1-70b-instruct": true,
	"bytedance/seed-oss-36b-instruct":          true,
	"deepseek-ai/deepseek-v4-flash":            true,
	"deepseek-ai/deepseek-v4-pro":              true,
	"google/gemma-2-2b-it":                     true,
	"google/gemma-3n-e2b-it":                   true,
	"google/gemma-3n-e4b-it":                   true,
	"google/gemma-4-31b-it":                    true,
	"meta/llama-3.2-1b-instruct":               true,
	"meta/llama-4-maverick-17b-128e-instruct":  true,
	"microsoft/phi-4-mini-instruct":            true,
	"minimaxai/minimax-m2.7":                   true,
	"mistralai/mistral-nemotron":               true,
	"nvidia/nemotron-mini-4b-instruct":         true,
	"qwen/qwen3-next-80b-a3b-instruct":         true,
	"qwen/qwen3.5-397b-a17b":                   true,
	"sarvamai/sarvam-m":                        true,
	"upstage/solar-10.7b-instruct":             true,
}

type NvidiaProvider struct {
	ProviderSkeleton
}

func (p *NvidiaProvider) Name() string { return types.ProviderNameNvidia }

func (p *NvidiaProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.Nvidia
	if pb == nil || ctx.NvidiaModelIDs == nil {
		return nil
	}
	liveIDs := ctx.NvidiaModelIDs

	var result []types.Model
	for modelID, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		if m.Modalities == nil || !Contains(m.Modalities.Input, "text") || !Contains(m.Modalities.Output, "text") {
			continue
		}

		liveID := liveIDs[modelID]
		if liveID == "" {
			normalized := strings.ToLower(strings.ReplaceAll(modelID, "_", "."))
			liveID = liveIDs[normalized]
		}
		if liveID == "" {
			continue
		}
		if unsupportedModels[liveID] {
			continue
		}

		model := BuildModel(liveID, m, "openai-completions", types.ProviderNameNvidia,
			"https://integrate.api.nvidia.com/v1")
		model.Headers = map[string]string{"NVCF-POLL-SECONDS": "3600"}
		model.Compat = map[string]any{
			"supportsStore":              false,
			"supportsDeveloperRole":      false,
			"supportsReasoningEffort":    false,
			"maxTokensField":             "max_tokens",
			"supportsStrictMode":         false,
			"supportsLongCacheRetention": false,
		}
		result = append(result, model)
	}
	return result
}

func (p *NvidiaProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *NvidiaProvider) Inject() []types.Model {
	return nil
}
