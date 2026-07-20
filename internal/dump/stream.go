package dump

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5"
)

// slowCheckpoint records the last successfully streamed primary-key value
// so a slow-connection dump can resume after interruption.
// LastPK is stored as json.Number to keep arbitrary integer precision.
type slowCheckpoint struct {
	Schema    string   `json:"schema,omitempty"`
	Table     string   `json:"table"`
	PKColumns []string `json:"pk_columns,omitempty"`
	PKColumn  string   `json:"pk_column,omitempty"` // legacy single-PK format; detected and discarded
	// ponytail: json.Number cannot hold text PKs; store JSON number|string per column.
	LastPK []any `json:"last_pk"`
}

func checkpointPath(dir, table string) string {
	return filepath.Join(dir, table+".ckpt.json")
}

func slowArtifactStem(table db.Table) string {
	return hex.EncodeToString([]byte(table.Schema)) + "." + hex.EncodeToString([]byte(table.Name))
}

func slowCheckpointPath(dir string, table db.Table) string {
	return checkpointPath(dir, slowArtifactStem(table))
}

func rejectAmbiguousLegacySlowArtifacts(dir string, tables []db.Table) error {
	counts := make(map[string]int, len(tables))
	for _, table := range tables {
		counts[table.Name]++
	}
	for name, count := range counts {
		if count < 2 {
			continue
		}
		for _, path := range []string{checkpointPath(dir, name), checkpointPath(dir, name) + ".tmp", filepath.Join(dir, name+".ndjson.tmp")} {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("ambiguous legacy slow artifact %q for same-named tables", path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat legacy slow artifact %q: %w", path, err)
			}
		}
	}
	return nil
}

func loadSlowCheckpoint(path string) (*slowCheckpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var cp slowCheckpoint
	if err := dec.Decode(&cp); err == nil && (len(cp.LastPK) > 0 || cp.PKColumn == "") {
		return &cp, nil
	}
	var leg struct {
		Table    string      `json:"table"`
		PKColumn string      `json:"pk_column"`
		LastPK   json.Number `json:"last_pk"`
	}
	if err := json.Unmarshal(data, &leg); err != nil {
		return nil, err
	}
	return &slowCheckpoint{Table: leg.Table, PKColumn: leg.PKColumn}, nil
}

func saveSlowCheckpoint(path string, table db.Table, pkCols []string, lastPk []any) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	nums := make([]any, len(lastPk))
	for i, v := range lastPk {
		nums[i] = checkpointStoreValue(v)
	}
	cp := slowCheckpoint{Schema: table.Schema, Table: table.Name, PKColumns: pkCols, LastPK: nums}
	if err := json.NewEncoder(f).Encode(cp); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func toJSONNumber(v any) json.Number {
	switch n := v.(type) {
	case json.Number:
		return n
	case string:
		return json.Number(n)
	case int:
		return json.Number(strconv.Itoa(n))
	case int32:
		return json.Number(strconv.FormatInt(int64(n), 10))
	case int64:
		return json.Number(strconv.FormatInt(n, 10))
	case uint:
		return json.Number(strconv.FormatUint(uint64(n), 10))
	case uint64:
		return json.Number(strconv.FormatUint(n, 10))
	case float64:
		return json.Number(strconv.FormatFloat(n, 'f', -1, 64))
	}
	return json.Number(fmt.Sprintf("%v", v))
}

func isIntegerPKType(dataType string) bool {
	switch strings.ToLower(dataType) {
	case "bigint", "integer", "smallint", "bigserial", "serial", "smallserial",
		"int", "int8", "int4", "int2":
		return true
	}
	return false
}

func checkpointStoreValue(v any) any {
	switch n := v.(type) {
	case int, int32, int64, uint, uint64, float64:
		return toJSONNumber(n)
	case json.Number:
		return n
	case string:
		return n
	default:
		return fmt.Sprintf("%v", v)
	}
}

func checkpointPKValues(vals []any, pkTypes []string) ([]any, error) {
	if len(vals) != len(pkTypes) {
		return nil, fmt.Errorf("checkpoint PK count %d does not match column count %d", len(vals), len(pkTypes))
	}
	out := make([]any, len(vals))
	for i, v := range vals {
		if isIntegerPKType(pkTypes[i]) {
			n := toJSONNumber(v)
			i64, err := n.Int64()
			if err != nil {
				return nil, fmt.Errorf("parse checkpoint PK[%d] %q as int64: %w", i, n, err)
			}
			if err != nil {
				return nil, fmt.Errorf("parse checkpoint PK %q as int64: %w", n, err)
			}
			out[i] = i64
		} else {
			s, err := checkpointAsString(v)
			if err != nil {
				return nil, fmt.Errorf("parse checkpoint PK[%d]: %w", i, err)
			}
			out[i] = s
		}
	}
	return out, nil
}

func checkpointAsString(v any) (string, error) {
	switch n := v.(type) {
	case string:
		return n, nil
	case json.Number:
		return n.String(), nil
	default:
		return "", fmt.Errorf("expected string checkpoint value, got %T", v)
	}
}

func readLastNonEmptyLine(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("empty temp file")
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return "", fmt.Errorf("empty temp file")
	}
	return last, nil
}

func parseSlowTempRow(line string) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func validateSlowCheckpointTemp(tmpPath string, pkCols []string, cp *slowCheckpoint) error {
	line, err := readLastNonEmptyLine(tmpPath)
	if err != nil {
		return fmt.Errorf("validate checkpoint temp for table %q: %w", cp.Table, err)
	}
	row, err := parseSlowTempRow(line)
	if err != nil {
		return fmt.Errorf("validate checkpoint temp for table %q: last row is not valid JSON: %w", cp.Table, err)
	}
	if len(pkCols) != len(cp.LastPK) {
		return fmt.Errorf("validate checkpoint temp for table %q: pk column count mismatch", cp.Table)
	}
	for i, col := range pkCols {
		v, ok := row[col]
		if !ok {
			return fmt.Errorf("validate checkpoint temp for table %q: last row missing pk column %q", cp.Table, col)
		}
		got, err := checkpointAsString(v)
		if err != nil {
			return fmt.Errorf("validate checkpoint temp for table %q: last row pk %q: %w", cp.Table, col, err)
		}
		want, err := checkpointAsString(cp.LastPK[i])
		if err != nil {
			return fmt.Errorf("validate checkpoint temp for table %q: checkpoint last_pk[%d]: %w", cp.Table, i, err)
		}
		if got != want {
			return fmt.Errorf("validate checkpoint temp for table %q: checkpoint last_pk[%d] %s does not match temp last row %s=%s", cp.Table, i, want, col, got)
		}
	}
	return nil
}

// DefaultSlowChunkSize is the number of rows per keyset-pagination chunk in slow-connection mode.
const DefaultSlowChunkSize = 1000

// ValidateTableName returns an error if name is empty or contains path
// separators or traversal patterns.
func ValidateTableName(name string) error {
	if name == "" {
		return fmt.Errorf("table name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("table name must not be a relative path component")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("table name must not contain path separators")
	}
	return nil
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func applyRowTransform(table db.Table, rowTransform RowTransform, rowMap map[string]any) (map[string]any, error) {
	if rowTransform == nil {
		return rowMap, nil
	}
	out, err := rowTransform(table.Schema, table.Name, table.Columns, rowMap)
	if err != nil {
		return nil, fmt.Errorf("transform row for table %q: %w", table.Name, err)
	}
	if out == nil {
		return nil, fmt.Errorf("transform row for table %q: nil row", table.Name)
	}
	return preserveColumnSet(table, rowMap, out), nil
}

// preserveColumnSet enforces the original table column keys on transform output.
// Transforms may add or remove keys; the dump pipeline always writes the source column set.
func preserveColumnSet(table db.Table, original, transformed map[string]any) map[string]any {
	out := make(map[string]any, len(table.Columns))
	for _, col := range table.Columns {
		if v, ok := transformed[col.Name]; ok {
			out[col.Name] = v
		} else if v, ok := original[col.Name]; ok {
			out[col.Name] = v
		} else {
			out[col.Name] = nil
		}
	}
	return out
}

func streamTable(ctx context.Context, q querier, table db.Table, dir string, rowTransform RowTransform) error {
	if err := ValidateTableName(table.Name); err != nil {
		return err
	}
	cols := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		cols[i] = pgx.Identifier{c.Name}.Sanitize()
	}

	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(cols, ", "), tableIdent)

	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query table %q: %w", table.Name, err)
	}
	defer rows.Close()

	finalPath := tableDataPath(dir, table)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	tmpPath := finalPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create file for table %q: %w", table.Name, err)
	}
	defer f.Close()

	succeeded := false
	defer func() {
		if !succeeded {
			os.Remove(tmpPath)
		}
	}()

	w := bufio.NewWriter(f)

	colNames := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		colNames[i] = c.Name
	}

	if rowTransform != nil {
		logSanitizationWarning(table)
	}

	for rows.Next() {
		values := make([]any, len(table.Columns))
		valuePtrs := make([]any, len(table.Columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("scan row for table %q: %w", table.Name, err)
		}

		rowMap := make(map[string]any, len(table.Columns))
		for i, name := range colNames {
			rowMap[name] = values[i]
		}

		rowMap, err := applyRowTransform(table, rowTransform, rowMap)
		if err != nil {
			return err
		}

		data, err := json.Marshal(rowMap)
		if err != nil {
			return fmt.Errorf("marshal row for table %q: %w", table.Name, err)
		}

		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("write row for table %q: %w", table.Name, err)
		}
		if err := w.WriteByte('\n'); err != nil {
			return fmt.Errorf("write row for table %q: %w", table.Name, err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows for table %q: %w", table.Name, err)
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table %q: %w", table.Name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close file for table %q: %w", table.Name, err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename table %q: %w", table.Name, err)
	}
	succeeded = true

	return nil
}

func streamTableFiltered(ctx context.Context, q querier, table db.Table, dir string, clauses []compiledWhere, rowTransform RowTransform, orderCol string) error {
	if err := ValidateTableName(table.Name); err != nil {
		return err
	}
	cols := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		cols[i] = pgx.Identifier{c.Name}.Sanitize()
	}

	whereSQL, args, err := mergeWhereClauses(clauses, 1)
	if err != nil {
		return fmt.Errorf("build where for table %q: %w", table.Name, err)
	}

	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s", strings.Join(cols, ", "), tableIdent, whereSQL)
	if orderCol != "" {
		orderIdent := pgx.Identifier{orderCol}.Sanitize()
		query += fmt.Sprintf(" ORDER BY %s", orderIdent)
	}

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query table %q: %w", table.Name, err)
	}
	defer rows.Close()

	finalPath := tableDataPath(dir, table)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	tmpPath := finalPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create file for table %q: %w", table.Name, err)
	}
	defer f.Close()

	succeeded := false
	defer func() {
		if !succeeded {
			os.Remove(tmpPath)
		}
	}()

	w := bufio.NewWriter(f)

	colNames := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		colNames[i] = c.Name
	}

	if rowTransform != nil {
		logSanitizationWarning(table)
	}

	for rows.Next() {
		values := make([]any, len(table.Columns))
		valuePtrs := make([]any, len(table.Columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("scan row for table %q: %w", table.Name, err)
		}

		rowMap := make(map[string]any, len(table.Columns))
		for i, name := range colNames {
			rowMap[name] = values[i]
		}

		rowMap, err := applyRowTransform(table, rowTransform, rowMap)
		if err != nil {
			return err
		}

		data, err := json.Marshal(rowMap)
		if err != nil {
			return fmt.Errorf("marshal row for table %q: %w", table.Name, err)
		}

		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("write row for table %q: %w", table.Name, err)
		}
		if err := w.WriteByte('\n'); err != nil {
			return fmt.Errorf("write row for table %q: %w", table.Name, err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows for table %q: %w", table.Name, err)
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table %q: %w", table.Name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close file for table %q: %w", table.Name, err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename table %q: %w", table.Name, err)
	}
	succeeded = true

	return nil
}

// streamTableSlow dumps a table in primary-key chunks using keyset pagination
// with checkpoint/resume support. Tables must have at least one primary-key
// column; missing PKs return a clear error.
// Each chunk queries up to chunkSize rows with keyset pagination on the PK tuple.
//
// Checkpoint behaviour:
//   - If table.ndjson already exists, the table is skipped (already done).
//   - If table.ndjson.tmp and table.ckpt.json both exist, streaming resumes
//     from the recorded last PK values, appending to the existing temp file.
//   - After each chunk is flushed, the checkpoint is updated.
//   - On successful completion the temp file is atomically renamed and the
//     checkpoint is removed.
//   - On any streaming error the temp file and checkpoint are preserved so
//     the next run can resume.
func streamTableSlow(ctx context.Context, q querier, table db.Table, dir string, rowTransform RowTransform, retry slowRetryConfig, chunkSize int) error {
	if err := ValidateTableName(table.Name); err != nil {
		return err
	}

	finalPath := tableDataPath(dir, table)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	ckptPath := slowCheckpointPath(dir, table)
	tmpPath := finalPath + ".tmp"
	legacyCkptPath := checkpointPath(dir, table.Name)
	legacyTmpPath := filepath.Join(dir, table.Name+".ndjson.tmp")
	legacyPaths := []string{legacyCkptPath, legacyCkptPath + ".tmp"}
	if legacyTmpPath != tmpPath {
		legacyPaths = append(legacyPaths, legacyTmpPath)
	}
	for _, legacyPath := range legacyPaths {
		if _, err := os.Stat(legacyPath); err == nil {
			tmpPath = legacyTmpPath
			ckptPath = legacyCkptPath
			break
		}
	}
	if _, err := os.Stat(finalPath); err == nil {
		// ponytail: already completed — skip re-streaming and clean stale artifacts.
		os.Remove(ckptPath)
		os.Remove(ckptPath + ".tmp")
		os.Remove(tmpPath)
		return nil
	}

	pkCols, err := primaryKeysColumns(table)
	if err != nil {
		return fmt.Errorf("slow-connection mode: %w", err)
	}

	cols := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		cols[i] = pgx.Identifier{c.Name}.Sanitize()
	}

	pkIndices := make([]int, len(pkCols))
	pkIdents := make([]string, len(pkCols))
	pkTypes := make([]string, len(pkCols))
	for j, pkCol := range pkCols {
		pkIdents[j] = pgx.Identifier{pkCol}.Sanitize()
		found := false
		for i, c := range table.Columns {
			if c.Name == pkCol {
				pkIndices[j] = i
				pkTypes[j] = c.DataType
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("slow-connection mode: pk column %q not found on table %q", pkCol, table.Name)
		}
	}

	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	pkOrder := strings.Join(pkIdents, ", ")
	colList := strings.Join(cols, ", ")

	colNames := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		colNames[i] = c.Name
	}

	var lastPk []any
	appendMode := false
	if _, err := os.Stat(tmpPath); os.IsNotExist(err) {
		// No temp data file: any checkpoint/temp-checkpoint is stale.
		os.Remove(ckptPath)
		os.Remove(ckptPath + ".tmp")
	} else if cp, err := loadSlowCheckpoint(ckptPath); err == nil {
		if len(cp.PKColumns) == 0 && cp.PKColumn != "" {
			// ponytail: legacy single-PK checkpoint — discard and restart fresh.
			os.Remove(tmpPath)
			os.Remove(ckptPath)
			os.Remove(ckptPath + ".tmp")
		} else if err := validateSlowCheckpointTemp(tmpPath, pkCols, cp); err != nil {
			// ponytail: corrupt/mismatched checkpoint+temp — reset and start fresh.
			os.Remove(tmpPath)
			os.Remove(ckptPath)
			os.Remove(ckptPath + ".tmp")
		} else {
			v, err := checkpointPKValues(cp.LastPK, pkTypes)
			if err != nil {
				return fmt.Errorf("load checkpoint for table %q: %w", table.Name, err)
			}
			lastPk = v
			appendMode = true
		}
	}

	var f *os.File
	if appendMode {
		f, err = os.OpenFile(tmpPath, os.O_APPEND|os.O_WRONLY, 0o600)
	} else {
		f, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	}
	if err != nil {
		return fmt.Errorf("create file for table %q: %w", table.Name, err)
	}
	defer f.Close()

	// ponytail: O_APPEND keeps the fd offset at 0, so f.SeekCurrent returns 0
	// and would truncate away committed rows on a first-chunk rollback. Capture
	// the real file size now; subsequent chunks update the offset after flush.
	var rollbackOffset int64
	if appendMode {
		fi, err := f.Stat()
		if err != nil {
			return fmt.Errorf("stat temp file for table %q: %w", table.Name, err)
		}
		rollbackOffset = fi.Size()
	}
	resumed := appendMode

	w := bufio.NewWriter(f)

	if rowTransform != nil {
		logSanitizationWarning(table)
	}

	maxAttempts := retry.max
	baseDelay := retry.base
	const maxBackoff = 30 * time.Second // ponytail: cap to avoid hours-long waits

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var query string
		var args []any
		if lastPk == nil {
			query = fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT %d",
				colList, tableIdent, pkOrder, chunkSize)
		} else {
			placeholders := make([]string, len(lastPk))
			for i := range lastPk {
				placeholders[i] = fmt.Sprintf("$%d", i+1)
			}
			query = fmt.Sprintf("SELECT %s FROM %s WHERE (%s) > (%s) ORDER BY %s LIMIT %d",
				colList, tableIdent, pkOrder, strings.Join(placeholders, ", "), pkOrder, chunkSize)
			args = lastPk
		}

		// Record file offset before writing the chunk. If checkpoint save fails
		// after the flush, we truncate back to this offset to avoid duplicates.
		if err := w.Flush(); err != nil {
			return fmt.Errorf("flush table %q: %w", table.Name, err)
		}
		var offset int64
		if resumed {
			offset = rollbackOffset
			resumed = false
		} else {
			offset, err = f.Seek(0, io.SeekCurrent)
			if err != nil {
				return fmt.Errorf("seek table %q: %w", table.Name, err)
			}
		}

		chunkDone, chunkErr := func() (bool, error) {
			var rows *sql.Rows
			for attempt := 0; ; attempt++ {
				var err error
				rows, err = q.QueryContext(ctx, query, args...)
				if err == nil {
					break
				}
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				if maxAttempts == 0 || attempt >= maxAttempts {
					return false, fmt.Errorf("query table %q chunk: %w", table.Name, err)
				}
				backoff := baseDelay << uint(attempt)
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return false, ctx.Err()
				}
			}
			defer rows.Close()

			rowCount := 0
			var chunkLines [][]byte
			var chunkLast []any
			for rows.Next() {
				values := make([]any, len(table.Columns))
				valuePtrs := make([]any, len(table.Columns))
				for i := range values {
					valuePtrs[i] = &values[i]
				}

				if err := rows.Scan(valuePtrs...); err != nil {
					return false, fmt.Errorf("scan row for table %q: %w", table.Name, err)
				}

				rowMap := make(map[string]any, len(table.Columns))
				for i, name := range colNames {
					rowMap[name] = values[i]
				}

				rowMap, err := applyRowTransform(table, rowTransform, rowMap)
				if err != nil {
					return false, err
				}

				data, err := json.Marshal(rowMap)
				if err != nil {
					return false, fmt.Errorf("marshal row for table %q: %w", table.Name, err)
				}

				chunkLines = append(chunkLines, data)
				chunkLast = make([]any, len(pkIndices))
				for j, idx := range pkIndices {
					chunkLast[j] = values[idx]
				}
				rowCount++
			}

			if err := rows.Err(); err != nil {
				return false, fmt.Errorf("iterate rows for table %q: %w", table.Name, err)
			}

			for _, data := range chunkLines {
				if _, err := w.Write(data); err != nil {
					return false, fmt.Errorf("write row for table %q: %w", table.Name, err)
				}
				if err := w.WriteByte('\n'); err != nil {
					return false, fmt.Errorf("write row for table %q: %w", table.Name, err)
				}
			}
			if rowCount > 0 {
				lastPk = chunkLast
			}

			return rowCount < chunkSize, nil
		}()
		if chunkErr != nil {
			return chunkErr
		}

		// Persist progress after each chunk so we can resume.
		if err := w.Flush(); err != nil {
			return fmt.Errorf("flush table %q: %w", table.Name, err)
		}
		if lastPk != nil {
			if err := saveSlowCheckpoint(ckptPath, table, pkCols, lastPk); err != nil {
				// ponytail: checkpoint failed after flush; truncate the chunk so
				// the next resume cannot see duplicate rows.
				_ = f.Truncate(offset)
				return fmt.Errorf("save checkpoint for table %q: %w", table.Name, err)
			}
		}

		if chunkDone {
			break
		}
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table %q: %w", table.Name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close file for table %q: %w", table.Name, err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename table %q: %w", table.Name, err)
	}

	// ponytail: best-effort cleanup — checkpoint/temp no longer needed after rename.
	os.Remove(ckptPath)
	os.Remove(ckptPath + ".tmp")

	return nil
}
