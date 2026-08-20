package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type MiniMaxCNProvider struct {
	ProviderSkeleton
}

func (p *MiniMaxCNProvider) Name() string { return types.ProviderNameMiniMaxCN }

func (p *MiniMaxCNProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.MiniMaxCN
	if pb == nil {
		return nil
	}
	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		model := BuildModel(id, m, "anthropic-messages", types.ProviderNameMiniMaxCN,
			"https://api.minimaxi.com/anthropic")
		result = append(result, model)
	}
	return result
}

func (p *MiniMaxCNProvider) Patch(models []types.Model) []types.Model {
	minimaxCNSupported := map[string]bool{
		"MiniMax-M2.7":           true,
		"MiniMax-M2.7-highspeed": true,
		"MiniMax-M3":             true,
	}
	filtered := make([]types.Model, 0, len(models))
	for _, m := range models {
		if minimaxCNSupported[m.ID] {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func (p *MiniMaxCNProvider) Inject() []types.Model {
	return nil
}
