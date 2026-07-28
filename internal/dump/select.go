package dump

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/VicenteOlmos/dolly/internal/db"
)

// ErrTableSelection marks planning failures from include/exclude resolution.
var ErrTableSelection = errors.New("table selection")

// IsTableSelectionError reports whether err is a table selection planning failure.
func IsTableSelectionError(err error) bool {
	return errors.Is(err, ErrTableSelection)
}

// QualifiedTable is an exact schema.table selector.
type QualifiedTable struct {
	Schema string
	Name   string
}

func (t QualifiedTable) key() string {
	return tableKey(t.Schema, t.Name)
}

// Normalized returns a deterministic schema.table label for provenance.
func (t QualifiedTable) Normalized() string {
	return qualifiedName(t.Schema, t.Name)
}

// SelectorSource records where a selector value came from.
type SelectorSource struct {
	Kind string // "flag", "config", "file"
	Name string // flag name or file path
	Line int    // 1-based for file lines; 0 for flags/config
}

// SelectorEntry is one parsed include or exclude selector.
type SelectorEntry struct {
	Table  QualifiedTable
	Raw    string
	Source SelectorSource
}

// SelectionPolicy holds include/exclude selectors after CLI/config/file merge.
type SelectionPolicy struct {
	Includes []SelectorEntry
	Excludes []SelectorEntry
}

// SelectorRecord is one requested selector written to metadata provenance.
type SelectorRecord struct {
	Normalized string `json:"normalized"`
	Source     string `json:"source"`
}

// IgnoredFileLine records blank or comment lines skipped while loading a file.
type IgnoredFileLine struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Kind string `json:"kind"` // "blank" or "comment"
}

// TableSelectionProvenance records selection inputs and outcomes without credentials.
type TableSelectionProvenance struct {
	RequestedIncludes []SelectorRecord  `json:"requested_includes,omitempty"`
	RequestedExcludes []SelectorRecord  `json:"requested_excludes,omitempty"`
	Warnings          []string          `json:"warnings,omitempty"`
	Selected          []string          `json:"selected,omitempty"`
	IgnoredFileLines  []IgnoredFileLine `json:"ignored_file_lines,omitempty"`
}

// ParseQualifiedTable parses one exact schema.table selector.
// Unquoted schema.table requires exactly one dot; quoted components allow embedded dots.
func ParseQualifiedTable(raw string) (QualifiedTable, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return QualifiedTable{}, fmt.Errorf("empty table selector")
	}
	if strings.Contains(s, ",") {
		return QualifiedTable{}, fmt.Errorf("invalid table selector %q: CSV syntax is not supported", raw)
	}
	if hasGlobSyntax(s) {
		return QualifiedTable{}, fmt.Errorf("invalid table selector %q: glob syntax is not supported", raw)
	}

	var schema, name string
	var err error
	if strings.HasPrefix(s, `"`) {
		schema, s, err = parseQuotedIdent(s)
		if err != nil {
			return QualifiedTable{}, fmt.Errorf("invalid table selector %q: %w", raw, err)
		}
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(s, ".") {
			return QualifiedTable{}, fmt.Errorf("invalid table selector %q: unqualified name", raw)
		}
		s = strings.TrimSpace(s[1:])
		if s == "" {
			return QualifiedTable{}, fmt.Errorf("invalid table selector %q: missing table name", raw)
		}
		if strings.HasPrefix(s, `"`) {
			name, s, err = parseQuotedIdent(s)
		} else {
			name, s, err = parseUnquotedIdent(s)
		}
		if err != nil {
			return QualifiedTable{}, fmt.Errorf("invalid table selector %q: %w", raw, err)
		}
	} else {
		dot := strings.IndexByte(s, '.')
		if dot < 0 {
			return QualifiedTable{}, fmt.Errorf("invalid table selector %q: unqualified name", raw)
		}
		if strings.Contains(s[dot+1:], ".") {
			return QualifiedTable{}, fmt.Errorf("invalid table selector %q: extra dots require quoted identifiers", raw)
		}
		schema, err = parseUnquotedIdentComponent(s[:dot])
		if err != nil {
			return QualifiedTable{}, fmt.Errorf("invalid table selector %q: %w", raw, err)
		}
		name, err = parseUnquotedIdentComponent(s[dot+1:])
		if err != nil {
			return QualifiedTable{}, fmt.Errorf("invalid table selector %q: %w", raw, err)
		}
		s = ""
	}
	if strings.TrimSpace(s) != "" {
		return QualifiedTable{}, fmt.Errorf("invalid table selector %q: unexpected trailing input", raw)
	}
	if schema == "" || name == "" {
		return QualifiedTable{}, fmt.Errorf("invalid table selector %q: unqualified name", raw)
	}
	if isSystemSchema(schema) {
		return QualifiedTable{}, fmt.Errorf("invalid table selector %q: system schema %q is not allowed", raw, schema)
	}
	return QualifiedTable{Schema: schema, Name: name}, nil
}

func hasGlobSyntax(s string) bool {
	for _, ch := range s {
		switch ch {
		case '*', '?', '[', ']':
			return true
		}
	}
	return false
}

func parseQuotedIdent(s string) (ident, rest string, err error) {
	if !strings.HasPrefix(s, `"`) {
		return "", "", fmt.Errorf("expected quoted identifier")
	}
	var b strings.Builder
	i := 1
	for i < len(s) {
		if s[i] == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				b.WriteByte('"')
				i += 2
				continue
			}
			return b.String(), s[i+1:], nil
		}
		b.WriteByte(s[i])
		i++
	}
	return "", "", fmt.Errorf("unterminated quoted identifier")
}

func parseUnquotedIdent(s string) (ident, rest string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("expected identifier")
	}
	end := 0
	for end < len(s) && s[end] != '.' && !unicode.IsSpace(rune(s[end])) {
		end++
	}
	ident, err = parseUnquotedIdentComponent(s[:end])
	if err != nil {
		return "", "", err
	}
	return ident, strings.TrimSpace(s[end:]), nil
}

func parseUnquotedIdentComponent(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty identifier")
	}
	if !isIdentStart(rune(s[0])) {
		return "", fmt.Errorf("invalid identifier %q", s)
	}
	for _, ch := range s[1:] {
		if !isIdentPart(ch) {
			return "", fmt.Errorf("invalid identifier %q", s)
		}
	}
	return s, nil
}

func isIdentStart(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch)
}

func isIdentPart(ch rune) bool {
	return ch == '_' || ch == '$' || unicode.IsLetter(ch) || unicode.IsDigit(ch)
}

func isSystemSchema(schema string) bool {
	switch schema {
	case "pg_catalog", "information_schema", "pg_toast":
		return true
	}
	return strings.HasPrefix(schema, "pg_temp_") || strings.HasPrefix(schema, "pg_toast_")
}

func formatSelectorSource(src SelectorSource) string {
	switch src.Kind {
	case "file":
		if src.Line > 0 {
			return fmt.Sprintf("file:%s:%d", src.Name, src.Line)
		}
		return "file:" + src.Name
	default:
		return src.Name
	}
}

// LoadSelectorEntries parses direct values and file contents into selector entries.
func LoadSelectorEntries(direct []string, files []string, sourceKind, flagName string) ([]SelectorEntry, []IgnoredFileLine, error) {
	var entries []SelectorEntry
	var ignored []IgnoredFileLine

	for _, raw := range direct {
		table, err := ParseQualifiedTable(raw)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, SelectorEntry{
			Table: table,
			Raw:   strings.TrimSpace(raw),
			Source: SelectorSource{
				Kind: sourceKind,
				Name: flagName,
			},
		})
	}

	for _, path := range files {
		fileEntries, fileIgnored, err := loadSelectorFile(path, sourceKind)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, fileEntries...)
		ignored = append(ignored, fileIgnored...)
	}

	sort.Slice(entries, func(i, j int) bool {
		ki, kj := entries[i].Table.key(), entries[j].Table.key()
		if ki != kj {
			return ki < kj
		}
		return formatSelectorSource(entries[i].Source) < formatSelectorSource(entries[j].Source)
	})
	sort.Slice(ignored, func(i, j int) bool {
		if ignored[i].File != ignored[j].File {
			return ignored[i].File < ignored[j].File
		}
		return ignored[i].Line < ignored[j].Line
	})
	return entries, ignored, nil
}

func loadSelectorFile(path, sourceKind string) ([]SelectorEntry, []IgnoredFileLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read table file %q: %w", path, err)
	}
	defer f.Close()

	var entries []SelectorEntry
	var ignored []IgnoredFileLine
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			ignored = append(ignored, IgnoredFileLine{File: path, Line: lineNo, Kind: "blank"})
			continue
		}
		if strings.HasPrefix(line, "#") {
			ignored = append(ignored, IgnoredFileLine{File: path, Line: lineNo, Kind: "comment"})
			continue
		}
		table, err := ParseQualifiedTable(line)
		if err != nil {
			return nil, nil, fmt.Errorf("table file %q line %d: %w", path, lineNo, err)
		}
		entries = append(entries, SelectorEntry{
			Table: table,
			Raw:   line,
			Source: SelectorSource{
				Kind: sourceKind,
				Name: path,
				Line: lineNo,
			},
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read table file %q: %w", path, err)
	}
	return entries, ignored, nil
}

// BuildSelectionPolicy merges direct and file selectors into one policy.
func BuildSelectionPolicy(includeDirect, includeFiles, excludeDirect, excludeFiles []string) (*SelectionPolicy, []IgnoredFileLine, error) {
	return BuildSelectionPolicyWithSources(
		includeDirect, includeFiles, excludeDirect, excludeFiles,
		"flag", "--include-table", "flag", "--exclude-table",
	)
}

// BuildSelectionPolicyWithSources merges selectors and records provenance sources.
func BuildSelectionPolicyWithSources(
	includeDirect, includeFiles, excludeDirect, excludeFiles []string,
	includeKind, includeName, excludeKind, excludeName string,
) (*SelectionPolicy, []IgnoredFileLine, error) {
	includes, ignoredIncludes, err := LoadSelectorEntries(includeDirect, includeFiles, includeKind, includeName)
	if err != nil {
		return nil, nil, err
	}
	excludes, ignoredExcludes, err := LoadSelectorEntries(excludeDirect, excludeFiles, excludeKind, excludeName)
	if err != nil {
		return nil, nil, err
	}
	ignored := append(ignoredIncludes, ignoredExcludes...)
	if len(includes) == 0 && len(excludes) == 0 {
		return nil, ignored, nil
	}
	return &SelectionPolicy{Includes: includes, Excludes: excludes}, ignored, nil
}

// PlanTableSelection filters introspected tables using include-narrow/exclude-win semantics.
func PlanTableSelection(tables []db.Table, policy *SelectionPolicy, ignored []IgnoredFileLine) ([]db.Table, TableSelectionProvenance, error) {
	prov := TableSelectionProvenance{IgnoredFileLines: ignored}
	if policy == nil {
		return tables, prov, nil
	}

	byKey := make(map[string]db.Table, len(tables))
	for _, t := range tables {
		byKey[tableKey(t.Schema, t.Name)] = t
	}

	for _, e := range policy.Includes {
		prov.RequestedIncludes = append(prov.RequestedIncludes, SelectorRecord{
			Normalized: e.Table.Normalized(),
			Source:     formatSelectorSource(e.Source),
		})
	}
	for _, e := range policy.Excludes {
		prov.RequestedExcludes = append(prov.RequestedExcludes, SelectorRecord{
			Normalized: e.Table.Normalized(),
			Source:     formatSelectorSource(e.Source),
		})
	}
	sort.Slice(prov.RequestedIncludes, func(i, j int) bool {
		if prov.RequestedIncludes[i].Normalized != prov.RequestedIncludes[j].Normalized {
			return prov.RequestedIncludes[i].Normalized < prov.RequestedIncludes[j].Normalized
		}
		return prov.RequestedIncludes[i].Source < prov.RequestedIncludes[j].Source
	})
	sort.Slice(prov.RequestedExcludes, func(i, j int) bool {
		if prov.RequestedExcludes[i].Normalized != prov.RequestedExcludes[j].Normalized {
			return prov.RequestedExcludes[i].Normalized < prov.RequestedExcludes[j].Normalized
		}
		return prov.RequestedExcludes[i].Source < prov.RequestedExcludes[j].Source
	})

	selected := make(map[string]struct{}, len(tables))
	if len(policy.Includes) > 0 {
		seen := make(map[string]struct{}, len(policy.Includes))
		for _, inc := range policy.Includes {
			key := inc.Table.key()
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			if _, ok := byKey[key]; !ok {
				return nil, prov, fmt.Errorf("%w: include table %q not found in database", ErrTableSelection, inc.Table.Normalized())
			}
			selected[key] = struct{}{}
		}
	} else {
		for key := range byKey {
			selected[key] = struct{}{}
		}
	}

	excludeSeen := make(map[string]struct{}, len(policy.Excludes))
	for _, exc := range policy.Excludes {
		key := exc.Table.key()
		if _, dup := excludeSeen[key]; dup {
			continue
		}
		excludeSeen[key] = struct{}{}
		if _, ok := byKey[key]; !ok {
			prov.Warnings = append(prov.Warnings, fmt.Sprintf("exclude table %q not found in database", exc.Table.Normalized()))
			continue
		}
		delete(selected, key)
	}
	sort.Strings(prov.Warnings)

	if len(selected) == 0 {
		return nil, prov, fmt.Errorf("%w: table selection matched no tables", ErrTableSelection)
	}

	var filtered []db.Table
	for _, t := range tables {
		if _, ok := selected[tableKey(t.Schema, t.Name)]; ok {
			filtered = append(filtered, t)
			prov.Selected = append(prov.Selected, qualifiedName(t.Schema, t.Name))
		}
	}
	sort.Strings(prov.Selected)
	return filtered, prov, nil
}

func schemasFromTables(tables []db.Table) []string {
	seen := make(map[string]struct{})
	for _, t := range tables {
		if t.Schema != "" {
			seen[t.Schema] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
