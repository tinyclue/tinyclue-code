package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

const modelsDevURL = "https://models.dev/api.json"

// FetchModelsDev calls https://models.dev/api.json and returns the raw response.
func FetchModelsDev() (*types.ModelsDevResponse, error) {
	resp, err := httpClient.Get(modelsDevURL)
	if err != nil {
		return nil, fmt.Errorf("models.dev request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev returned status %d", resp.StatusCode)
	}

	var result types.ModelsDevResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("models.dev decode failed: %w", err)
	}

	return &result, nil
}
