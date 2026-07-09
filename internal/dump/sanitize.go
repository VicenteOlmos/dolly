package dump

import (
	"fmt"
	"os"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
)

// RowTransform mutates a single row's values before NDJSON serialization.
// Returning a nil row aborts the dump with an error.
type RowTransform func(schema, table string, columns []db.Column, row map[string]any) (map[string]any, error)

// WithRowTransform sets an optional row transform applied during streaming.
func WithRowTransform(fn RowTransform) Option {
	return func(c *config) {
		c.rowTransform = fn
	}
}

// SanitizationOptions returns dump options that enable the built-in column-pattern
// sanitizer when enabled is true.
func SanitizationOptions(enabled bool) []Option {
	if !enabled {
		return nil
	}
	return []Option{WithRowTransform(SanitizeByPattern)}
}

// sensitiveColumnNames lists exact column names (case-insensitive) that trigger
// built-in sanitization.
var sensitiveColumnNames = map[string]string{
	"email":                  "redacted@example.com",
	"email_address":          "redacted@example.com",
	"password":               "[REDACTED]",
	"passwd":                 "[REDACTED]",
	"password_hash":          "[REDACTED]",
	"ssn":                    "000-00-0000",
	"social_security":        "000-00-0000",
	"social_security_number": "000-00-0000",
	"phone":                  "+1-555-000-0000",
	"phone_number":           "+1-555-000-0000",
	"mobile":                 "+1-555-000-0000",
	"cellphone":              "+1-555-000-0000",
	"credit_card":            "xxxx-xxxx-xxxx-0000",
	"card_number":            "xxxx-xxxx-xxxx-0000",
	"cc_number":              "xxxx-xxxx-xxxx-0000",
	"secret":                 "[REDACTED]",
	"token":                  "[REDACTED]",
	"api_key":                "[REDACTED]",
	"api_secret":             "[REDACTED]",
	"access_token":           "[REDACTED]",
	"user_password":          "[REDACTED]",
	"auth_token":             "[REDACTED]",
	"refresh_token":          "[REDACTED]",
	"bearer_token":           "[REDACTED]",
	"private_key":            "[REDACTED]",
	"cvv":                    "[REDACTED]",
	"cvv_code":               "[REDACTED]",
	"iban":                   "[REDACTED]",
	"salt":                   "[REDACTED]",
	"mfa_code":               "[REDACTED]",
	"totp_secret":            "[REDACTED]",
	"recovery_code":          "[REDACTED]",
	"backup_code":            "[REDACTED]",
}

// sensitiveSubstrings triggers redaction for any column whose lowercased
// name contains one of these patterns. Catches names like
// api_token, client_secret, password_reset_token, etc.
var sensitiveSubstrings = []string{"token", "secret", "password", "passwd", "api_key", "private_key"}

// SanitizeByPattern replaces values in columns whose names match known
// sensitive patterns. For text-typed columns the replacement string is used.
// For non-text columns (bytea, numeric, uuid, jsonb, etc.) the value is
// set to nil — you cannot put a string replacement in a non-text column.
func SanitizeByPattern(_ string, _ string, columns []db.Column, row map[string]any) (map[string]any, error) {
	for _, col := range columns {
		replacement, matched := sensitiveReplacement(col.Name)
		if !matched {
			continue
		}
		val, exists := row[col.Name]
		if !exists || val == nil {
			continue
		}
		if isTextColumn(col.DataType) {
			switch val.(type) {
			case string, []byte:
				row[col.Name] = replacement
			}
		} else {
			// Non-text sensitive column: zero it out.
			row[col.Name] = nil
		}
	}
	return row, nil
}

func sensitiveReplacement(columnName string) (string, bool) {
	lower := strings.ToLower(columnName)
	if rep, ok := sensitiveColumnNames[lower]; ok {
		return rep, true
	}
	for _, sub := range sensitiveSubstrings {
		if strings.Contains(lower, sub) {
			return "[REDACTED]", true
		}
	}
	return "", false
}

func isTextColumn(dataType string) bool {
	dt := strings.ToLower(dataType)
	return dt == "text" ||
		strings.Contains(dt, "char") ||
		strings.Contains(dt, "citext")
}

// sensitiveColumns returns column names in table that would be sanitized
// (exact name match or substring match). Includes non-text-triggered columns
// so warnings cover all redacted columns.
func sensitiveColumns(columns []db.Column) []string {
	var names []string
	for _, col := range columns {
		if _, ok := sensitiveReplacement(col.Name); ok {
			names = append(names, col.Name)
		}
	}
	return names
}

func logSanitizationWarning(table db.Table) {
	cols := sensitiveColumns(table.Columns)
	if len(cols) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "sanitization: table %s: redacting columns %s\n", table.Name, strings.Join(cols, ", "))
}
