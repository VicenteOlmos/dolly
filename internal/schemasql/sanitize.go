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

// Sanitize removes compatibility-breaking SET lines for older restore targets.
// It is not a security validation boundary for schema SQL.
func Sanitize(in []byte) ([]byte, error) {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(in))
	for sc.Scan() {
		line := sc.Text()
		if shouldStripSchemaSetLine(line) {
			continue
		}
		out.WriteString(line)
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
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan schema.sql: %w", err)
	}
	return nil
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
