package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

const openRouterURL = "https://openrouter.ai/api/v1/models"

// FetchOpenRouter calls https://openrouter.ai/api/v1/models and returns the raw response.
func FetchOpenRouter() (*types.OpenRouterResponse, error) {
	resp, err := httpClient.Get(openRouterURL)
	if err != nil {
		return nil, fmt.Errorf("openrouter request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter returned status %d", resp.StatusCode)
	}

	var result types.OpenRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openrouter decode failed: %w", err)
	}

	return &result, nil
}
