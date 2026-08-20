package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

const nvidiaURL = "https://integrate.api.nvidia.com/v1/models"

// FetchNvidia calls https://integrate.api.nvidia.com/v1/models and returns the raw response.
func FetchNvidia() (*types.NvidiaResponse, error) {
	req, err := http.NewRequest(http.MethodGet, nvidiaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("nvidia request creation failed: %w", err)
	}
	req.Header.Set("NVCF-POLL-SECONDS", "3600")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nvidia request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nvidia returned status %d", resp.StatusCode)
	}

	var result types.NvidiaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("nvidia decode failed: %w", err)
	}

	return &result, nil
}

// FetchNvidiaModelIDs calls the NVIDIA NIM API and returns a lookup map
// keyed by the original and normalized (lowercased, _ → .) model ID.
func FetchNvidiaModelIDs() (map[string]string, error) {
	resp, err := FetchNvidia()
	if err != nil {
		return nil, err
	}

	ids := make(map[string]string, len(resp.Data))
	for _, m := range resp.Data {
		ids[m.ID] = m.ID
		normalized := strings.ToLower(strings.ReplaceAll(m.ID, "_", "."))
		ids[normalized] = m.ID
	}
	return ids, nil
}
