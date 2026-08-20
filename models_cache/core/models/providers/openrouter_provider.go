package providers

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/tinyclue/tinyclue-code/log"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

// OpenRouterProvider handles models from the OpenRouter API.
type OpenRouterProvider struct {
	ProviderSkeleton
}

func (p *OpenRouterProvider) Name() string { return types.ProviderNameOpenRouter }

// Filter fetches OpenRouter models and filters to tool-capable ones.
func (p *OpenRouterProvider) Filter(ctx *types.PipelineContext) []types.Model {
	data := ctx.OpenRouterData
	if data == nil {
		return nil
	}

	var result []types.Model
	for _, m := range data.Data {
		if !Contains(m.SupportedParameters, "tools") {
			continue
		}

		input := []string{"text"}
		if m.Architecture != nil && strings.Contains(m.Architecture.Modality, "image") {
			input = append(input, "image")
		}

		reasoning := Contains(m.SupportedParameters, "reasoning")

		ctxWindow := 4096
		if m.ContextLength > 0 {
			ctxWindow = m.ContextLength
		}

		maxTokens := 4096
		if m.TopProvider != nil && m.TopProvider.MaxCompletionTokens > 0 {
			maxTokens = m.TopProvider.MaxCompletionTokens
		}

		model := types.Model{
			ID:            m.ID,
			Name:          m.Name,
			API:           "openai-completions",
			BaseURL:       "https://openrouter.ai/api/v1",
			Provider:      types.ProviderNameOpenRouter,
			Reasoning:     reasoning,
			Input:         input,
			ContextWindow: ctxWindow,
			MaxTokens:     maxTokens,
			Cost: types.ModelCost{
				Input:      openRouterCost(m.Pricing, "prompt"),
				Output:     openRouterCost(m.Pricing, "completion"),
				CacheRead:  openRouterCost(m.Pricing, "input_cache_read"),
				CacheWrite: openRouterCost(m.Pricing, "input_cache_write"),
			},
		}
		result = append(result, model)
	}

	log.Infof(context.Background(), "OpenRouter: %d tool-capable models", len(result))
	return result
}

// Patch applies cost corrections and compat merges for OpenRouter models.
func (p *OpenRouterProvider) Patch(models []types.Model) []types.Model {
	for i, m := range models {
		if m.ID == "moonshotai/kimi-k2.5" {
			models[i].Cost.Input = 0.41
			models[i].Cost.Output = 2.06
			models[i].Cost.CacheRead = 0.07
			models[i].MaxTokens = 4096
		}
		if strings.HasPrefix(m.ID, "moonshotai/kimi-k2.6") {
			MergeCompat(&models[i], map[string]any{
				"supportsDeveloperRole":                       false,
				"requiresReasoningContentOnAssistantMessages": true,
			})
		}
		if m.ID == "z-ai/glm-5" {
			models[i].Cost.Input = 0.6
			models[i].Cost.Output = 1.9
			models[i].Cost.CacheRead = 0.119
		}
	}
	return models
}

// Inject returns the "auto" model if not already present.
func (p *OpenRouterProvider) Inject() []types.Model {
	return []types.Model{
		{
			ID:            "auto",
			Name:          "Auto",
			API:           "openai-completions",
			BaseURL:       "https://openrouter.ai/api/v1",
			Provider:      types.ProviderNameOpenRouter,
			Reasoning:     true,
			Input:         []string{"text", "image"},
			ContextWindow: 2000000,
			MaxTokens:     30000,
		},
	}
}

// openRouterCost extracts a pricing field from OpenRouter, converts from $/token
// to $/million tokens, and rounds to 6 decimal places.
func openRouterCost(pricing *types.OpenRouterPricing, field string) float64 {
	if pricing == nil {
		return 0
	}
	var raw string
	switch field {
	case "prompt":
		raw = pricing.Prompt
	case "completion":
		raw = pricing.Completion
	case "input_cache_read":
		raw = pricing.InputCacheRead
	case "input_cache_write":
		raw = pricing.InputCacheWrite
	}
	if raw == "" {
		return 0
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil || val <= 0 {
		return 0
	}
	perMillion := val * 1_000_000
	return math.Round(perMillion*1_000_000) / 1_000_000
}
