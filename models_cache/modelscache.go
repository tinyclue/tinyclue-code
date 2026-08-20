package models_cache

import (
	"context"
	"github.com/tinyclue/tinyclue-code/log"

	"github.com/tinyclue/tinyclue-code/models_cache/core"
	"github.com/tinyclue/tinyclue-code/models_cache/core/api"
	"github.com/tinyclue/tinyclue-code/models_cache/core/models"
	"github.com/tinyclue/tinyclue-code/models_cache/core/models/azureopenairesponses"
	"github.com/tinyclue/tinyclue-code/models_cache/core/models/providers"
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

func Run() (map[string]map[string]types.Model, error) {

	pipelineContext := &types.PipelineContext{}

	modelsDevData, err := api.FetchModelsDev()
	if err != nil {
		log.Errorf(context.Background(), "FetchModelsDev error: %v", err)
		return nil, err
	}
	nvidiaModelIDs, err2 := api.FetchNvidiaModelIDs()
	if err2 != nil {
		log.Errorf(context.Background(), "FetchNvidiaModelIDs error: %v", err2)
		return nil, err2
	}

	aiGatewayResponse, err3 := api.FetchAiGateway()
	if err3 != nil {
		log.Warnf(context.Background(), "Warning: AI Gateway fetch failed: %v", err3)
		return nil, err3
	}

	openRouterData, err4 := api.FetchOpenRouter()
	if err4 != nil {
		log.Warnf(context.Background(), "Warning: OpenRouter fetch failed: %v", err4)
		return nil, err4
	}

	pipelineContext.ModelsDevData = modelsDevData
	pipelineContext.NvidiaModelIDs = nvidiaModelIDs
	pipelineContext.AiGatewayData = aiGatewayResponse
	pipelineContext.OpenRouterData = openRouterData

	p := core.NewPipeline()
	p.AddProvider(&providers.AmazonBedrockProvider{})
	p.AddProvider(&providers.AnthropicProvider{})
	p.AddProvider(&providers.GoogleProvider{})
	p.AddProvider(&providers.OpenAIProvider{})
	p.AddProvider(&providers.GroqProvider{})
	p.AddProvider(&providers.CerebrasProvider{})
	p.AddProvider(&providers.CloudflareWorkersAIProvider{})
	p.AddProvider(&providers.CloudflareAIGatewayProvider{})
	p.AddProvider(&providers.XAIProvider{})
	p.AddProvider(&providers.ZAICodingPlanProvider{})
	p.AddProvider(&providers.MistralProvider{})
	p.AddProvider(&providers.HuggingFaceProvider{})
	p.AddProvider(&providers.FireworksAIProvider{})
	p.AddProvider(&providers.NvidiaProvider{})
	p.AddProvider(&providers.TogetherProvider{})
	p.AddProvider(&providers.OpenCodeProvider{})
	p.AddProvider(&providers.OpenCodeGoProvider{})
	p.AddProvider(&providers.GitHubCopilotProvider{})
	p.AddProvider(&providers.MiniMaxProvider{})
	p.AddProvider(&providers.MiniMaxCNProvider{})
	p.AddProvider(&providers.KimiForCodingProvider{})
	p.AddProvider(&providers.MoonshotAIProvider{})
	p.AddProvider(&providers.MoonshotAICNProvider{})
	p.AddProvider(&providers.XiaomiProvider{})
	// Inject-only providers
	p.AddProvider(&providers.DeepSeekProvider{})
	p.AddProvider(&providers.AntLingProvider{})
	p.AddProvider(&providers.OpenAICodexProvider{})
	p.AddProvider(&providers.GoogleVertexProvider{})
	// Self-fetching providers (no models.dev data)
	p.AddProvider(&providers.OpenRouterProvider{})
	p.AddProvider(&providers.AiGatewayProvider{})

	all, _ := p.Run(pipelineContext)

	// Azure clone: openai-responses → azure-openai-responses
	all = azureopenairesponses.CloneModels(all)

	// Global post-processing: DeepSeek compat, thinkingLevelMap, dedup.
	all = models.FinalizeModels(all)

	log.Infof(context.Background(), "Pipeline total: %d models", len(all))

	// Convert to nested map: providers[provider][id] = model
	result := map[string]map[string]types.Model{}
	for _, m := range all {
		if result[m.Provider] == nil {
			result[m.Provider] = map[string]types.Model{}
		}
		if _, exists := result[m.Provider][m.ID]; !exists {
			result[m.Provider][m.ID] = m
		}
	}
	return result, nil
}
