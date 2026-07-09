package dump

import (
	"encoding/json"
	"fmt"
	"os"
)

type seedFileJSON struct {
	Seeds  []RowPredicate `json:"seeds"`
	Limits SubsetLimits   `json:"limits"`
}

// ParseSeedFile loads subset configuration from a JSON seed file.
func ParseSeedFile(path string) (SubsetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SubsetConfig{}, fmt.Errorf("read seed file: %w", err)
	}
	var sf seedFileJSON
	if err := json.Unmarshal(data, &sf); err != nil {
		return SubsetConfig{}, fmt.Errorf("decode seed file: %w", err)
	}
	normalized := make([]RowPredicate, len(sf.Seeds))
	for i, s := range sf.Seeds {
		normalized[i] = normalizePredicate(s)
	}
	return SubsetConfig{
		Seeds:  normalized,
		Limits: sf.Limits,
	}, nil
}
