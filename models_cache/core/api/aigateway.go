package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

const aiGatewayModelsURL = "https://ai-gateway.vercel.sh/v1/models"

// FetchAiGateway calls https://ai-gateway.vercel.sh/v1/models and returns the raw response.
func FetchAiGateway() (*types.AiGatewayResponse, error) {
	resp, err := httpClient.Get(aiGatewayModelsURL)
	if err != nil {
		return nil, fmt.Errorf("ai-gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ai-gateway returned status %d", resp.StatusCode)
	}

	var result types.AiGatewayResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai-gateway decode failed: %w", err)
	}

	return &result, nil
}
