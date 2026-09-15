package schemasql

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// unsupportedSchemaSetVars are PostgreSQL GUCs newer pg_dump may emit that older
// servers reject during psql restore. Stripped from schema.sql before apply.
var unsupportedSchemaSetVars = map[string]struct{}{
	"transaction_timeout": {},
}

// Sanitize removes compatibility-breaking SET lines for older restore targets
// and rewrites CREATE SCHEMA to CREATE SCHEMA IF NOT EXISTS so replay into a
// fresh database (which already has public) does not abort.
// It is not a security validation boundary for schema SQL.
func Sanitize(in []byte) ([]byte, error) {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(in))
	for sc.Scan() {
		line := sc.Text()
		if shouldStripSchemaSetLine(line) {
			continue
		}
		out.WriteString(rewriteCreateSchemaIfNotExists(line))
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan schema.sql: %w", err)
	}
	return out.Bytes(), nil
}

// SanitizeReader streams sanitized schema SQL to w.
func SanitizeReader(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if shouldStripSchemaSetLine(line) {
			continue
		}
		if _, err := io.WriteString(w, rewriteCreateSchemaIfNotExists(line)+"\n"); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan schema.sql: %w", err)
	}
	return nil
}

const createSchemaKeyword = "create schema"
const ifNotExistsKeyword = "if not exists"

// rewriteCreateSchemaIfNotExists inserts IF NOT EXISTS into a CREATE SCHEMA
// statement. pg_dump emits a bare CREATE SCHEMA public; which fails against
// every newly created database. Lines that already have IF NOT EXISTS, or that
// are comments, are left unchanged.
func rewriteCreateSchemaIfNotExists(line string) string {
	start := 0
	for start < len(line) && (line[start] == ' ' || line[start] == '\t') {
		start++
	}
	if start >= len(line) || line[start] == '-' {
		return line
	}
	rest := line[start:]
	if len(rest) < len(createSchemaKeyword) || !strings.EqualFold(rest[:len(createSchemaKeyword)], createSchemaKeyword) {
		return line
	}
	after := rest[len(createSchemaKeyword):]
	i := 0
	for i < len(after) && (after[i] == ' ' || after[i] == '\t') {
		i++
	}
	namePart := after[i:]
	if len(namePart) >= len(ifNotExistsKeyword) && strings.EqualFold(namePart[:len(ifNotExistsKeyword)], ifNotExistsKeyword) {
		return line
	}
	ws := after[:i]
	if ws == "" {
		ws = " "
	}
	return line[:start] + rest[:len(createSchemaKeyword)] + ws + "IF NOT EXISTS " + namePart
}

func shouldStripSchemaSetLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || trimmed[0] == '-' {
		return false
	}
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "set ") {
		return false
	}
	rest := strings.TrimSpace(lower[4:])
	if strings.HasPrefix(rest, "local ") {
		rest = strings.TrimSpace(rest[6:])
	}
	eq := strings.IndexByte(rest, '=')
	if eq <= 0 {
		return false
	}
	name := strings.TrimSpace(rest[:eq])
	if strings.HasPrefix(name, "session authorization") {
		return false
	}
	_, strip := unsupportedSchemaSetVars[name]
	return strip
}
