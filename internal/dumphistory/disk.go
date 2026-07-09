package dumphistory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// ListBaseMerged returns store records for baseDir plus unregistered numbered
// subdirectories that contain metadata.json (e.g. from CLI runs before history existed).
func ListBaseMerged(baseDir string, store Store) ([]Record, error) {
	var stored []Record
	if store != nil {
		recs, err := store.ListBase(baseDir)
		if err != nil {
			return nil, err
		}
		stored = recs
	}

	byPath := make(map[string]Record, len(stored))
	for _, r := range stored {
		byPath[normalizePath(r.Path)] = r
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return sortRecords(stored), nil
		}
		return nil, err
	}

	normBase := normalizePath(baseDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		seq, err := strconv.Atoi(e.Name())
		if err != nil || seq <= 0 {
			continue
		}
		path := filepath.Join(baseDir, e.Name())
		if _, ok := byPath[normalizePath(path)]; ok {
			continue
		}
		rec, ok := recordFromDisk(normBase, seq, path)
		if !ok {
			continue
		}
		stored = append(stored, rec)
		byPath[normalizePath(path)] = rec
	}

	return sortRecords(stored), nil
}

func sortRecords(recs []Record) []Record {
	out := append([]Record(nil), recs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seq != out[j].Seq {
			return out[i].Seq > out[j].Seq
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

type diskMetadata struct {
	Schema     string          `json:"schema"`
	Tables     []diskTable     `json:"tables"`
	Provenance *diskProvenance `json:"provenance,omitempty"`
}

type diskTable struct {
	RowCount *int64 `json:"row_count"`
}

type diskProvenance struct {
	SourceDatabase   string   `json:"source_database"`
	Schemas          []string `json:"schemas"`
	TableCount       int      `json:"table_count"`
	TotalRowEstimate int64    `json:"total_row_estimate"`
}

func recordFromDisk(baseDir string, seq int, path string) (Record, bool) {
	metaPath := filepath.Join(path, "metadata.json")
	info, err := os.Stat(metaPath)
	if err != nil {
		return Record{}, false
	}
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return Record{}, false
	}
	var meta diskMetadata
	if err := json.Unmarshal(data, &meta); err != nil || meta.Schema == "" {
		return Record{}, false
	}

	tableCount := len(meta.Tables)
	var rowEst int64
	for _, t := range meta.Tables {
		if t.RowCount != nil {
			rowEst += *t.RowCount
		}
	}
	sourceDB := ""
	var schemas []string
	if meta.Provenance != nil {
		sourceDB = meta.Provenance.SourceDatabase
		schemas = append([]string(nil), meta.Provenance.Schemas...)
		if meta.Provenance.TableCount > 0 {
			tableCount = meta.Provenance.TableCount
		}
		if meta.Provenance.TotalRowEstimate > 0 {
			rowEst = meta.Provenance.TotalRowEstimate
		}
	}

	return Record{
		Seq:            seq,
		BaseDir:        baseDir,
		Path:           path,
		CreatedAt:      info.ModTime().UTC(),
		SourceDatabase: sourceDB,
		Schemas:        schemas,
		SchemaLabel:    meta.Schema,
		TableCount:     tableCount,
		RowEstimate:    rowEst,
	}, true
}
