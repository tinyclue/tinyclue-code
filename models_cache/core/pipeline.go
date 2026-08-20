package core

import (
	"context"
	"github.com/tinyclue/tinyclue-code/log"

	"github.com/tinyclue/tinyclue-code/models_cache/core/models/providers"
	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

// Pipeline orchestrates the full model-cache pipeline: fetch from APIs,
// run registered providers through the three-phase pipeline
// (Filter → Patch → Inject), then apply global post-processing.
type Pipeline struct {
	providers []providers.ModelProvider
}

// NewPipeline creates an empty Pipeline.
// Use AddProvider to register providers before calling Run.
func NewPipeline() *Pipeline {
	return &Pipeline{}
}

// AddProvider appends a ModelProvider to the pipeline's provider list.
func (p *Pipeline) AddProvider(provider providers.ModelProvider) {
	p.providers = append(p.providers, provider)
}

// Run executes the complete pipeline and returns models grouped by provider.
func (p *Pipeline) Run(ctx *types.PipelineContext) ([]types.Model, error) {

	var all []types.Model

	// Run each provider through Filter → Patch → Inject.
	for _, provider := range p.providers {
		providerModels := provider.Filter(ctx)
		providerModels = provider.Patch(providerModels)
		providerModels = append(providerModels, provider.Inject()...)
		all = append(all, providerModels...)
	}

	log.Infof(context.Background(), "Models.dev pipeline: %d models after filtering", len(all))
	return all, nil
}
