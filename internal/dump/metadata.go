package dump

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func assignDataFiles(tables []db.Table) {
	for i := range tables {
		path := "data/" + hex.EncodeToString([]byte(tables[i].Schema)) + "." + hex.EncodeToString([]byte(tables[i].Name)) + ".ndjson"
		tables[i].DataFile = &path
	}
}

func tableDataPath(dir string, table db.Table) string {
	if table.DataFile != nil {
		return filepath.Join(dir, *table.DataFile)
	}
	return filepath.Join(dir, table.Name+".ndjson")
}

// SubsetManifest records how a subset dump was produced.
type SubsetManifest struct {
	Seeds        []RowPredicate `json:"seeds"`
	Limits       SubsetLimits   `json:"limits"`
	Tables       []string       `json:"tables"`
	RowsExported map[string]int `json:"rows_exported"`
	Percent      int            `json:"percent,omitempty"`
}

// SequenceState records a sequence's last value for restoration.
type SequenceState struct {
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	LastValue  *int64 `json:"last_value,omitempty"`
	StartValue int64  `json:"start_value"`
	IsCalled   bool   `json:"is_called"`
}

// Metadata describes a dump's generation time, schema, and tables.
type Metadata struct {
	GeneratedAt string          `json:"generated_at"`
	Schema      string          `json:"schema"`
	Tables      []db.Table      `json:"tables"`
	Subset      *SubsetManifest `json:"subset,omitempty"`
	Provenance  *Provenance     `json:"provenance,omitempty"`
	Sequences   []SequenceState `json:"sequences,omitempty"`
}

// Provenance records dump identity and source context for history/restore tracking.
type Provenance struct {
	Seq              int      `json:"seq"`
	BaseDir          string   `json:"base_dir"`
	SourceDatabase   string   `json:"source_database,omitempty"`
	SourceSignature  string   `json:"source_signature,omitempty"`
	Schemas          []string `json:"schemas,omitempty"`
	Sanitized        *bool    `json:"sanitization_enabled,omitempty"`
	TableCount       int      `json:"table_count"`
	TotalRowEstimate int64    `json:"total_row_estimate,omitempty"`
	TableSelection   *TableSelectionProvenance `json:"table_selection,omitempty"`
}

func writeMetadata(dir string, tables []db.Table, subset *SubsetManifest, filterSchemas []string, sequences []SequenceState, prov *Provenance) (string, error) {
	m := Metadata{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Schema:      metadataSchemaLabel(filterSchemas, tables),
		Tables:      tables,
		Subset:      subset,
		Provenance:  prov,
		Sequences:   sequences,
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	tmpPath := filepath.Join(dir, "metadata.json.tmp")
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write metadata: %w", err)
	}

	return tmpPath, nil
}

// ReadMetadata loads metadata.json from a dump directory.
func ReadMetadata(dir string) (Metadata, error) {
	path := filepath.Join(dir, "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata: %w", err)
	}

	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return Metadata{}, fmt.Errorf("decode metadata: %w", err)
	}
	if m.Schema == "" {
		return Metadata{}, fmt.Errorf("metadata: missing schema")
	}
	return m, nil
}

func metadataSchemaLabel(filterSchemas []string, tables []db.Table) string {
	if len(filterSchemas) == 1 {
		return filterSchemas[0]
	}
	seen := make(map[string]struct{})
	for _, t := range tables {
		if t.Schema != "" {
			seen[t.Schema] = struct{}{}
		}
	}
	switch len(seen) {
	case 0:
		return "public"
	case 1:
		for s := range seen {
			return s
		}
	}
	return "multi"
}
