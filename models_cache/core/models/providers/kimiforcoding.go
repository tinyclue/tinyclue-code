package providers

import (
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

type KimiForCodingProvider struct {
	ProviderSkeleton
}

func (p *KimiForCodingProvider) Name() string { return types.ProviderNameKimiForCoding }

func (p *KimiForCodingProvider) Filter(ctx *types.PipelineContext) []types.Model {
	pb := ctx.ModelsDevData.KimiForCoding
	if pb == nil {
		return nil
	}
	const canonicalID = "kimi-for-coding"
	hasCanonical := false
	for id := range pb.Models {
		if id == canonicalID {
			hasCanonical = true
			break
		}
	}

	aliases := map[string]bool{"k2p5": true, "k2p6": true}

	var result []types.Model
	for id, m := range pb.Models {
		if m.ToolCall == nil || !*m.ToolCall {
			continue
		}
		if aliases[id] && hasCanonical {
			continue
		}

		normalizedID := id
		normalizedName := m.Name
		if aliases[id] {
			normalizedID = canonicalID
			normalizedName = "Kimi For Coding"
		}

		model := BuildModel(normalizedID, m, "anthropic-messages", "kimi-coding",
			"https://api.kimi.com/coding")
		model.Name = normalizedName
		model.Headers = map[string]string{"User-Agent": "KimiCLI/1.5"}
		result = append(result, model)
	}
	return result
}

func (p *KimiForCodingProvider) Patch(models []types.Model) []types.Model {
	return models
}

func (p *KimiForCodingProvider) Inject() []types.Model {
	return nil
}
