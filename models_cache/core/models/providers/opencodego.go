package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type OpenCodeGoProvider struct {
	ProviderSkeleton
}

func (p *OpenCodeGoProvider) Name() string { return types.ProviderNameOpenCodeGo }

func (p *OpenCodeGoProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.OpenCodeGo
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		if m.Status != nil && *m.Status == "deprecated" {
			continue
		}
		if id == "gpt-5.3-codex-spark" {
			continue
		}

		npm := ""
		if m.Provider != nil && m.Provider.NPM != nil {
			npm = *m.Provider.NPM
		}

		var api, baseURL string
		var compat map[string]any

		switch npm {
		case "@ai-sdk/openai":
			api = "openai-responses"
			baseURL = "https://opencode.ai/zen/go/v1"
		case "@ai-sdk/anthropic":
			api = "anthropic-messages"
			baseURL = "https://opencode.ai/zen/go"
		case "@ai-sdk/google":
			api = "google-generative-ai"
			baseURL = "https://opencode.ai/zen/go/v1"
		case "@ai-sdk/alibaba":
			api = "openai-completions"
			baseURL = "https://opencode.ai/zen/go/v1"
			compat = map[string]any{"cacheControlFormat": "anthropic"}
		default:
			api = "openai-completions"
			baseURL = "https://opencode.ai/zen/go/v1"
		}

		if id == "kimi-k2.6" {
			if compat == nil {
				compat = map[string]any{}
			}
			compat["thinkingFormat"] = "deepseek"
			compat["supportsReasoningEffort"] = false
		}

		if id == "minimax-m2.7" {
			api = "openai-completions"
			baseURL = "https://opencode.ai/zen/go/v1"
		}
		if id == "qwen3.5-plus" || id == "qwen3.6-plus" {
			api = "openai-completions"
			baseURL = "https://opencode.ai/zen/go/v1"
			if compat == nil {
				compat = map[string]any{}
			}
			compat["thinkingFormat"] = "qwen"
		}

		if api == "openai-completions" {
			if compat == nil {
				compat = map[string]any{}
			}
			compat["maxTokensField"] = "max_tokens"
		}

		model := BuildModel(id, m, api, types.ProviderNameOpenCodeGo, baseURL)
		if compat != nil {
			model.Compat = compat
		}
		result = append(result, model)
	}
	return result
}

func (p *OpenCodeGoProvider) Patch(models []types.Model) []types.Model {
	for i, m := range models {
		if m.ID == "claude-opus-4-6" || m.ID == "claude-sonnet-4-6" ||
			m.ID == "claude-opus-4.6" || m.ID == "claude-sonnet-4.6" {
			models[i].ContextWindow = 1000000
		}
		if m.ID == "claude-sonnet-4-5" || m.ID == "claude-sonnet-4" {
			models[i].ContextWindow = 200000
		}
		if m.ID == "gpt-5.4" {
			models[i].ContextWindow = 272000
			models[i].MaxTokens = 128000
		}
	}
	return models
}

func (p *OpenCodeGoProvider) Inject() []types.Model {
	return nil
}
