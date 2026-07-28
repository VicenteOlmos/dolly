package dump

import (
	"errors"
	"fmt"
	"sort"

	"github.com/VicenteOlmos/dolly/internal/db"
)

// ErrChunkPolicy marks planning failures from chunk-table resolution.
var ErrChunkPolicy = errors.New("chunk table policy")

// IsChunkPolicyError reports whether err is a chunk-table planning failure.
func IsChunkPolicyError(err error) bool {
	return errors.Is(err, ErrChunkPolicy)
}

// ChunkPolicy holds chunk-table selectors after CLI/config/file merge.
type ChunkPolicy struct {
	Requests []SelectorEntry
}

// ChunkTableProvenance records chunk inputs and outcomes without credentials.
type ChunkTableProvenance struct {
	Requested        []SelectorRecord  `json:"requested,omitempty"`
	Chunked          []string          `json:"chunked,omitempty"`
	IgnoredFileLines []IgnoredFileLine `json:"ignored_file_lines,omitempty"`
}

// BuildChunkPolicy merges direct and file chunk selectors into one policy.
func BuildChunkPolicy(direct, files []string) (*ChunkPolicy, []IgnoredFileLine, error) {
	return BuildChunkPolicyWithSources(direct, files, "flag", "--chunk-table")
}

// BuildChunkPolicyWithSources merges chunk selectors and records provenance sources.
func BuildChunkPolicyWithSources(direct, files []string, sourceKind, flagName string) (*ChunkPolicy, []IgnoredFileLine, error) {
	requests, ignored, err := LoadSelectorEntries(direct, files, sourceKind, flagName)
	if err != nil {
		return nil, nil, err
	}
	if len(requests) == 0 {
		return nil, ignored, nil
	}
	return &ChunkPolicy{Requests: requests}, ignored, nil
}

// PlanChunkStreaming resolves chunk selectors against selected tables, preflights
// primary keys, and returns the set of qualified tables that must use keyset streaming.
func PlanChunkStreaming(tables []db.Table, policy *ChunkPolicy, ignored []IgnoredFileLine) (map[string]struct{}, ChunkTableProvenance, error) {
	prov := ChunkTableProvenance{IgnoredFileLines: ignored}
	chunked := make(map[string]struct{})
	if policy == nil || len(policy.Requests) == 0 {
		return chunked, prov, nil
	}

	byKey := make(map[string]db.Table, len(tables))
	for _, t := range tables {
		byKey[tableKey(t.Schema, t.Name)] = t
	}

	for _, req := range policy.Requests {
		prov.Requested = append(prov.Requested, SelectorRecord{
			Normalized: req.Table.Normalized(),
			Source:     formatSelectorSource(req.Source),
		})
	}
	sort.Slice(prov.Requested, func(i, j int) bool {
		if prov.Requested[i].Normalized != prov.Requested[j].Normalized {
			return prov.Requested[i].Normalized < prov.Requested[j].Normalized
		}
		return prov.Requested[i].Source < prov.Requested[j].Source
	})

	seen := make(map[string]struct{}, len(policy.Requests))
	for _, req := range policy.Requests {
		key := req.Table.key()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		table, ok := byKey[key]
		if !ok {
			return nil, prov, fmt.Errorf("%w: chunk table %q not found in selected tables", ErrChunkPolicy, req.Table.Normalized())
		}
		if _, err := primaryKeysColumns(table); err != nil {
			return nil, prov, fmt.Errorf("%w: chunk table %q: %w", ErrChunkPolicy, req.Table.Normalized(), err)
		}
		chunked[key] = struct{}{}
		prov.Chunked = append(prov.Chunked, qualifiedName(table.Schema, table.Name))
	}
	sort.Strings(prov.Chunked)
	return chunked, prov, nil
}
