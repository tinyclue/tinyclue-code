package output

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tinyclue/tinyclue-code/models_cache/core/types"
)

// SaveJSON serializes models (nested map format) to a JSON file at the given path.
func SaveJSON(providers map[string]map[string]types.Model, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(providers); err != nil {
		return fmt.Errorf("encode models: %w", err)
	}
	return nil
}
