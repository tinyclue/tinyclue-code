package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type MiniMaxProvider struct {
	ProviderSkeleton
}

func (p *MiniMaxProvider) Name() string { return types.ProviderNameMiniMax }

func (p *MiniMaxProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.MiniMax
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "anthropic-messages", types.ProviderNameMiniMax,
			"https://api.minimax.io/anthropic")
		result = append(result, model)
	}
	return result
}

func (p *MiniMaxProvider) Patch(models []types.Model) []types.Model {
	minimaxSupported := map[string]bool{
		"MiniMax-M2.7":           true,
		"MiniMax-M2.7-highspeed": true,
		"MiniMax-M3":             true,
	}
	filtered := make([]types.Model, 0, len(models))
	for _, m := range models {
		if minimaxSupported[m.ID] {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func (p *MiniMaxProvider) Inject() []types.Model {
	return nil
}
