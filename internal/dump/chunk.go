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
	Fallback         []string          `json:"fallback,omitempty"`
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

// PlanChunkStreaming resolves exact chunk selectors into deterministic per-table
// key plans. Tables without a safe key receive an explicit normal-stream plan.
func PlanChunkStreaming(tables []db.Table, policy *ChunkPolicy, ignored []IgnoredFileLine) (map[string]KeyDescriptor, ChunkTableProvenance, error) {
	prov := ChunkTableProvenance{IgnoredFileLines: ignored}
	plans := make(map[string]KeyDescriptor)
	if policy == nil || len(policy.Requests) == 0 {
		return plans, prov, nil
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

	for _, req := range policy.Requests {
		key := req.Table.key()
		if _, planned := plans[key]; planned {
			continue
		}

		table, ok := byKey[key]
		if !ok {
			return nil, prov, fmt.Errorf("%w: chunk table %q not found in selected tables", ErrChunkPolicy, req.Table.Normalized())
		}
		plan := SelectKeyDescriptor(table)
		plans[key] = plan
		qualified := qualifiedName(table.Schema, table.Name)
		switch plan.Strategy {
		case KeyStrategyPrimaryKey:
			prov.Chunked = append(prov.Chunked, qualified)
		case KeyStrategyNormalStream:
			prov.Fallback = append(prov.Fallback, qualified)
		}
	}
	sort.Strings(prov.Chunked)
	sort.Strings(prov.Fallback)
	return plans, prov, nil
}

// ChunkPolicyResumeFingerprint records chunk selector inputs for resumable dump matching.
func ChunkPolicyResumeFingerprint(policy *ChunkPolicy) *ChunkTableProvenance {
	if policy == nil || len(policy.Requests) == 0 {
		return nil
	}
	prov := &ChunkTableProvenance{}
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
	return prov
}

// ChunkResumeProvenanceMatches reports whether interrupted and current chunk policies match.
func ChunkResumeProvenanceMatches(expect, got *ChunkTableProvenance) bool {
	if expect == nil && (got == nil || len(got.Requested) == 0) {
		return true
	}
	if expect == nil || got == nil || len(expect.Requested) == 0 || len(got.Requested) == 0 {
		return false
	}
	if len(expect.Requested) != len(got.Requested) {
		return false
	}
	for i := range expect.Requested {
		if expect.Requested[i].Normalized != got.Requested[i].Normalized {
			return false
		}
	}
	return true
}
