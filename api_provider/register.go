package api_provider

import (
	"sync"

	"github.com/tinyclue/tinyclue-code/api_provider/provider/adapter"
	"github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/config"
	modelstypes "github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type adapterCtor func(name, baseURL, apiKey, model string, modelCost *types.ModelCost, maxTokens, contextWindow int, authType string) types.ProtocolAdapter

// apiAdapters maps model-cache api types to adapter constructors.
// Add a new entry here when a new protocol adapter is implemented.
var apiAdapters = map[string]adapterCtor{
	"openai-completions": func(name, baseURL, apiKey, model string, modelCost *types.ModelCost, maxTokens, contextWindow int, authType string) types.ProtocolAdapter {
		return adapter.NewOpenAIAdapter(name, baseURL, apiKey, model, modelCost, maxTokens, contextWindow, authType)
	},
	"openai-responses": func(name, baseURL, apiKey, model string, modelCost *types.ModelCost, maxTokens, contextWindow int, authType string) types.ProtocolAdapter {
		return adapter.NewOpenAIAdapter(name, baseURL, apiKey, model, modelCost, maxTokens, contextWindow, authType)
	},
	"openai-codex-responses": func(name, baseURL, apiKey, model string, modelCost *types.ModelCost, maxTokens, contextWindow int, authType string) types.ProtocolAdapter {
		return adapter.NewOpenAIAdapter(name, baseURL, apiKey, model, modelCost, maxTokens, contextWindow, authType)
	},
	"anthropic-messages": func(name, baseURL, apiKey, model string, modelCost *types.ModelCost, maxTokens, contextWindow int, authType string) types.ProtocolAdapter {
		return adapter.NewAnthropicAdapter(baseURL, apiKey, model, "2023-06-01", modelCost, maxTokens, contextWindow, authType)
	},
}

var registerOnce sync.Once

// registerProviders iterates all providers in model-cache.json and registers
// each with the appropriate adapter based on the model's api field.
func registerProviders() {
	registerOnce.Do(func() {
		for name, models := range config.Data {
			apiType := resolveAPIType(models)
			if apiType == "" {
				continue
			}
			ctor, ok := apiAdapters[apiType]
			if !ok {
				continue // no adapter registered for this api type
			}
			pName := name // capture for closure
			types.RegisterAdapter(pName, func(key, model, authType string) types.ProtocolAdapter {
				return ctor(pName, lookupBaseURL(pName, model), key, model, lookupModelCost(pName, model), lookupMaxTokens(pName, model), lookupContextWindow(pName, model), authType)
			})
		}
	})
}

// resolveAPIType returns the api type (e.g. "openai-completions", "anthropic-messages")
// from the first model in the map. All models under a provider are expected to share
// the same api type; mixed-api providers use whichever type is encountered first.
func resolveAPIType(models map[string]modelstypes.Model) string {
	for _, m := range models {
		if m.API != "" {
			return m.API
		}
	}
	return ""
}

// lookupBaseURL returns the base URL for a given provider+model from the model cache.
func lookupBaseURL(provider, model string) string {
	if models, ok := config.Data[provider]; ok {
		if m, ok := models[model]; ok && m.BaseURL != "" {
			return m.BaseURL
		}
		// Fallback: use any model's baseURL for this provider.
		for _, m := range models {
			if m.BaseURL != "" {
				return m.BaseURL
			}
		}
	}
	return ""
}

// lookupModelCost returns the ModelCost for a given provider+model from the model cache.
func lookupModelCost(provider, model string) *types.ModelCost {
	if models, ok := config.Data[provider]; ok {
		if m, ok := models[model]; ok {
			return &types.ModelCost{
				Input:      m.Cost.Input,
				Output:     m.Cost.Output,
				CacheRead:  m.Cost.CacheRead,
				CacheWrite: m.Cost.CacheWrite,
			}
		}
	}
	return nil
}

// lookupMaxTokens returns the max tokens for a given provider+model from the model cache.
func lookupMaxTokens(provider, model string) int {
	if models, ok := config.Data[provider]; ok {
		if m, ok := models[model]; ok {
			return m.MaxTokens
		}
		// Fallback: use any model's MaxTokens for this provider.
		for _, m := range models {
			if m.MaxTokens > 0 {
				return m.MaxTokens
			}
		}
	}
	return 0
}

// lookupContextWindow returns the context window for a given provider+model from the model cache.
func lookupContextWindow(provider, model string) int {
	if models, ok := config.Data[provider]; ok {
		if m, ok := models[model]; ok {
			return m.ContextWindow
		}
		// Fallback: use any model's ContextWindow for this provider.
		for _, m := range models {
			if m.ContextWindow > 0 {
				return m.ContextWindow
			}
		}
	}
	return 0
}
