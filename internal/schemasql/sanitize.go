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
// and rewrites top-level CREATE SCHEMA to CREATE SCHEMA IF NOT EXISTS so replay
// into a fresh database (which already has public) does not abort.
// CREATE SCHEMA inside dollar-quoted bodies, strings, or block comments is left
// unchanged. It is not a security validation boundary for schema SQL.
func Sanitize(in []byte) ([]byte, error) {
	var out bytes.Buffer
	var st sqlTextState
	sc := bufio.NewScanner(bytes.NewReader(in))
	for sc.Scan() {
		line := sc.Text()
		topLevel := !st.inQuotedOrComment()
		st.consume(line)
		if topLevel && shouldStripSchemaSetLine(line) {
			continue
		}
		if topLevel {
			line = rewriteCreateSchemaIfNotExists(line)
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
	var st sqlTextState
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		topLevel := !st.inQuotedOrComment()
		st.consume(line)
		if topLevel && shouldStripSchemaSetLine(line) {
			continue
		}
		if topLevel {
			line = rewriteCreateSchemaIfNotExists(line)
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

// sqlTextState tracks whether the scanner is inside a PostgreSQL string, dollar
// quote, or block comment so CREATE SCHEMA rewrites stay at top level.
type sqlTextState struct {
	dollarTag     string
	blockDepth    int
	inSingleQuote bool
	inIdentQuote  bool
}

func (s *sqlTextState) inQuotedOrComment() bool {
	return s.dollarTag != "" || s.blockDepth > 0 || s.inSingleQuote || s.inIdentQuote
}

func (s *sqlTextState) consume(line string) {
	i := 0
	for i < len(line) {
		switch {
		case s.inSingleQuote:
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i += 2
					continue
				}
				s.inSingleQuote = false
			}
			i++
		case s.inIdentQuote:
			if line[i] == '"' {
				if i+1 < len(line) && line[i+1] == '"' {
					i += 2
					continue
				}
				s.inIdentQuote = false
			}
			i++
		case s.dollarTag != "":
			if strings.HasPrefix(line[i:], s.dollarTag) {
				i += len(s.dollarTag)
				s.dollarTag = ""
				continue
			}
			i++
		case s.blockDepth > 0:
			if i+1 < len(line) && line[i] == '/' && line[i+1] == '*' {
				s.blockDepth++
				i += 2
				continue
			}
			if i+1 < len(line) && line[i] == '*' && line[i+1] == '/' {
				s.blockDepth--
				i += 2
				continue
			}
			i++
		default:
			if i+1 < len(line) && line[i] == '-' && line[i+1] == '-' {
				return
			}
			if i+1 < len(line) && line[i] == '/' && line[i+1] == '*' {
				s.blockDepth = 1
				i += 2
				continue
			}
			if delim, ok := dollarQuoteDelim(line, i); ok {
				s.dollarTag = delim
				i += len(delim)
				continue
			}
			switch line[i] {
			case '\'':
				s.inSingleQuote = true
			case '"':
				s.inIdentQuote = true
			}
			i++
		}
	}
}

func dollarQuoteDelim(s string, i int) (string, bool) {
	if i >= len(s) || s[i] != '$' {
		return "", false
	}
	j := i + 1
	if j < len(s) && (s[j] == '_' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= 'a' && s[j] <= 'z') {
		j++
		for j < len(s) && (s[j] == '_' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= '0' && s[j] <= '9') {
			j++
		}
	}
	if j < len(s) && s[j] == '$' {
		return s[i : j+1], true
	}
	return "", false
}

const createSchemaKeyword = "create schema"
const ifNotExistsKeyword = "if not exists"

// rewriteCreateSchemaIfNotExists inserts IF NOT EXISTS into a top-level CREATE
// SCHEMA statement. pg_dump emits a bare CREATE SCHEMA public; which fails
// against every newly created database. Lines that already have IF NOT EXISTS,
// or that are line comments, are left unchanged. Callers must skip quoted
// bodies and block comments.
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
