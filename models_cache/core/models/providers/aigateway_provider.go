package providers

import (
	"context"
	"math"
	"slices"
	"strconv"

	"github.com/tinyclue/tinyclue-code/log"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

// AiGatewayProvider handles models from the Vercel AI Gateway API.
type AiGatewayProvider struct {
	ProviderSkeleton
}

func (p *AiGatewayProvider) Name() string { return types.ProviderNameVercelAIGateway }

// Filter fetches AI Gateway models and filters to tool-use capable ones.
func (p *AiGatewayProvider) Filter(ctx *types.PipelineContext) []types.Model {
	data := ctx.AiGatewayData
	if data == nil {
		return nil
	}

	var result []types.Model
	for _, m := range data.Data {
		if !slices.Contains(m.Tags, "tool-use") {
			continue
		}

		input := []string{"text"}
		if slices.Contains(m.Tags, "vision") {
			input = append(input, "image")
		}

		reasoning := slices.Contains(m.Tags, "reasoning")

		ctxWindow := 4096
		if m.ContextWindow > 0 {
			ctxWindow = m.ContextWindow
		}

		maxTokens := 4096
		if m.MaxTokens > 0 {
			maxTokens = m.MaxTokens
		}

		name := m.Name
		if name == "" {
			name = m.ID
		}

		model := types.Model{
			ID:            m.ID,
			Name:          name,
			API:           "anthropic-messages",
			BaseURL:       "https://ai-gateway.vercel.sh",
			Provider:      types.ProviderNameVercelAIGateway,
			Reasoning:     reasoning,
			Input:         input,
			ContextWindow: ctxWindow,
			MaxTokens:     maxTokens,
			Cost: types.ModelCost{
				Input:      aiGatewayCost(m.Pricing, "input"),
				Output:     aiGatewayCost(m.Pricing, "output"),
				CacheRead:  aiGatewayCost(m.Pricing, "input_cache_read"),
				CacheWrite: aiGatewayCost(m.Pricing, "input_cache_write"),
			},
		}
		result = append(result, model)
	}

	log.Infof(context.Background(), "AI Gateway: %d tool-capable models", len(result))
	return result
}

// aiGatewayCost extracts a pricing field from AI Gateway, converts from
// $/token to $/million tokens, and rounds to 6 decimal places.
func aiGatewayCost(pricing *types.AiGatewayPricing, field string) float64 {
	if pricing == nil {
		return 0
	}
	var raw any
	switch field {
	case "input":
		raw = pricing.Input
	case "output":
		raw = pricing.Output
	case "input_cache_read":
		raw = pricing.InputCacheRead
	case "input_cache_write":
		raw = pricing.InputCacheWrite
	}
	if raw == nil {
		return 0
	}

	var val float64
	switch v := raw.(type) {
	case float64:
		val = v
	case string:
		if v == "" {
			return 0
		}
		var err error
		val, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return 0
		}
	default:
		return 0
	}

	if val <= 0 || math.IsInf(val, 0) || math.IsNaN(val) {
		return 0
	}

	perMillion := val * 1_000_000
	return math.Round(perMillion*1_000_000) / 1_000_000
}
