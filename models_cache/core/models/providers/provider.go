package providers

import "github.com/tinyclue/tinyclue-code/models_cache/core/types"

// ModelProvider defines the three-phase pipeline interface for a single provider.
//
// Phase 1 — Filter: transforms raw API data into model entries.
// Phase 2 — Patch: applies corrections/enrichments to the filtered models.
// Phase 3 — Inject: injects additional models not present in the API data.
//
// Providers that have no corresponding API data (inject-only) return nil from Filter.
type ModelProvider interface {
	// Name returns the provider identifier (e.g. "amazon-bedrock").
	Name() string
	// Filter extracts models from the PipelineContext (e.g. ctx.ModelsDevData).
	Filter(ctx *types.PipelineContext) []types.Model
	// Patch applies post-processing corrections to the filtered models.
	Patch(models []types.Model) []types.Model
	// Inject returns additional models not present in the API data.
	Inject() []types.Model
}

// ProviderSkeleton provides default (no-op) implementations so that providers
// only need to override the methods they actually implement.
type ProviderSkeleton struct{}

func (ProviderSkeleton) Filter(_ *types.PipelineContext) []types.Model { return nil }
func (ProviderSkeleton) Patch(models []types.Model) []types.Model      { return models }
func (ProviderSkeleton) Inject() []types.Model                         { return nil }
