package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type GoogleVertexProvider struct {
	ProviderSkeleton
}

func (p *GoogleVertexProvider) Name() string { return types.ProviderNameGoogleVertex }

func (p *GoogleVertexProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *GoogleVertexProvider) Inject() []types.Model {
	const baseURL = "https://{location}-aiplatform.googleapis.com"
	return []types.Model{
		{
			ID: "gemini-3-pro-preview", Name: "Gemini 3 Pro Preview (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1000000, MaxTokens: 64000,
			Cost: types.ModelCost{Input: 2, Output: 12, CacheRead: 0.2, CacheWrite: 0},
		},
		{
			ID: "gemini-3.1-pro-preview", Name: "Gemini 3.1 Pro Preview (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 65536,
			Cost: types.ModelCost{Input: 2, Output: 12, CacheRead: 0.2, CacheWrite: 0},
		},
		{
			ID: "gemini-3.1-pro-preview-customtools", Name: "Gemini 3.1 Pro Preview Custom Tools (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 65536,
			Cost: types.ModelCost{Input: 2, Output: 12, CacheRead: 0.2, CacheWrite: 0},
		},
		{
			ID: "gemini-3-flash-preview", Name: "Gemini 3 Flash Preview (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 65536,
			Cost: types.ModelCost{Input: 0.5, Output: 3, CacheRead: 0.05, CacheWrite: 0},
		},
		{
			ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: false, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 8192,
			Cost: types.ModelCost{Input: 0.15, Output: 0.6, CacheRead: 0.0375, CacheWrite: 0},
		},
		{
			ID: "gemini-2.0-flash-lite", Name: "Gemini 2.0 Flash Lite (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 65536,
			Cost: types.ModelCost{Input: 0.075, Output: 0.3, CacheRead: 0.01875, CacheWrite: 0},
		},
		{
			ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 65536,
			Cost: types.ModelCost{Input: 1.25, Output: 10, CacheRead: 0.125, CacheWrite: 0},
		},
		{
			ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 65536,
			Cost: types.ModelCost{Input: 0.3, Output: 2.5, CacheRead: 0.03, CacheWrite: 0},
		},
		{
			ID: "gemini-2.5-flash-lite-preview-09-2025", Name: "Gemini 2.5 Flash Lite Preview 09-25 (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 65536,
			Cost: types.ModelCost{Input: 0.1, Output: 0.4, CacheRead: 0.01, CacheWrite: 0},
		},
		{
			ID: "gemini-2.5-flash-lite", Name: "Gemini 2.5 Flash Lite (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: true, Input: []string{"text", "image"},
			ContextWindow: 1048576, MaxTokens: 65536,
			Cost: types.ModelCost{Input: 0.1, Output: 0.4, CacheRead: 0.01, CacheWrite: 0},
		},
		{
			ID: "gemini-1.5-pro", Name: "Gemini 1.5 Pro (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: false, Input: []string{"text", "image"},
			ContextWindow: 1000000, MaxTokens: 8192,
			Cost: types.ModelCost{Input: 1.25, Output: 5, CacheRead: 0.3125, CacheWrite: 0},
		},
		{
			ID: "gemini-1.5-flash", Name: "Gemini 1.5 Flash (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: false, Input: []string{"text", "image"},
			ContextWindow: 1000000, MaxTokens: 8192,
			Cost: types.ModelCost{Input: 0.075, Output: 0.3, CacheRead: 0.01875, CacheWrite: 0},
		},
		{
			ID: "gemini-1.5-flash-8b", Name: "Gemini 1.5 Flash-8B (Vertex)",
			API: "google-vertex", BaseURL: baseURL, Provider: types.ProviderNameGoogleVertex,
			Reasoning: false, Input: []string{"text", "image"},
			ContextWindow: 1000000, MaxTokens: 8192,
			Cost: types.ModelCost{Input: 0.0375, Output: 0.15, CacheRead: 0.01, CacheWrite: 0},
		},
	}
}
