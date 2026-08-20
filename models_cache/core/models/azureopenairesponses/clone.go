package azureopenairesponses

import "github.com/tinyclue/tinyclue-code/models_cache/core/types"

// CloneModels clones all openai+openai-responses models to azure-openai-responses.
func CloneModels(all []types.Model) []types.Model {
	azureCtxOverrides := map[string]int{
		"gpt-5.4": 1050000,
		"gpt-5.5": 1050000,
	}
	for _, m := range all {
		if m.Provider == types.ProviderNameOpenAI && m.API == "openai-responses" {
			clone := m
			clone.API = "azure-openai-responses"
			clone.Provider = "azure-openai-responses"
			clone.BaseURL = ""
			if override, ok := azureCtxOverrides[clone.ID]; ok {
				clone.ContextWindow = override
			}
			all = append(all, clone)
		}
	}
	return all
}
