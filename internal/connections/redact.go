package connections

import (
	"net/url"
	"regexp"
	"strings"
)

// redactParamRE matches password-like query params embedded in arbitrary text.
// Pattern: key=value (until whitespace/quote/end-of-string).
// Keys are matched against isSecretQueryKey.
var redactParamRE = regexp.MustCompile(`(?i)([&?])?(` + strings.Join(allSecretKeyNames(), "|") + `)=([^\s"']*)`)

// libpqKeywordRE matches libpq keyword=value pairs in arbitrary text.
var libpqKeywordRE = regexp.MustCompile(`(?i)\b(` + strings.Join(allSecretKeyNames(), "|") + `)=(?:'(?:[^'\\]|\\.)*'|[^\s"']*)`)

// postgresURLRE matches PostgreSQL URL DSNs embedded in arbitrary text.
var postgresURLRE = regexp.MustCompile(`postgres(?:ql)?://[^\s"']+`)

// RedactDSN returns a DSN string with passwords redacted — both in userinfo
// and in query parameters. Query keys matching known secret patterns
// (exact or substring) are replaced with "***". Non-secret params are
// preserved. Libpq keyword DSNs (host=... password=...) are redacted too.
// If the DSN cannot be parsed as a URL or contains no secrets it is returned
// unchanged.
func RedactDSN(dsn string) string {
	if redacted, ok := redactLibpqKeywordDSN(dsn); ok {
		return redacted
	}
	rawQueryRedacted := false
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		dsn, rawQueryRedacted = redactRawQuerySecrets(dsn)
	}
	u, err := parsePostgresURL(dsn)
	if err != nil {
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			return "postgresql://***"
		}
		return dsn
	}
	modified := rawQueryRedacted

	// Redact userinfo password.
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "***")
			modified = true
		}
	}

	// Redact password-like query parameters.
	q := u.Query()
	for key := range q {
		if isSecretQueryKey(key) {
			q.Set(key, "***")
			modified = true
		}
	}
	if !modified {
		return dsn
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// redactRawQuerySecrets handles malformed query escapes before url.Values drops them.
func redactRawQuerySecrets(dsn string) (string, bool) {
	start := strings.IndexByte(dsn, '?')
	if start < 0 {
		return dsn, false
	}
	end := strings.IndexByte(dsn[start:], '#')
	if end < 0 {
		end = len(dsn)
	} else {
		end += start
	}
	parts := strings.Split(dsn[start+1:end], "&")
	changed := false
	for i, part := range parts {
		key, _, ok := strings.Cut(part, "=")
		if ok && isSecretQueryKey(key) {
			parts[i] = key + "=***"
			changed = true
		}
	}
	if !changed {
		return dsn, false
	}
	return dsn[:start+1] + strings.Join(parts, "&") + dsn[end:], true
}

var secretQueryKeys = map[string]struct{}{
	"password":    {},
	"pass":        {},
	"pwd":         {},
	"sslpassword": {},
	"sslcert":     {},
	"sslkey":      {},
	"passcode":    {},
}

// secretSubstrings are patterns that, when found anywhere in a lowercased
// param key, cause the param to be redacted. These catch custom params like
// my_password, auth_token, client_cert, etc.
var secretSubstrings = []string{"pass", "secret", "token", "key", "cert"}

func isSecretQueryKey(key string) bool {
	lower := strings.ToLower(key)
	if _, ok := secretQueryKeys[lower]; ok {
		return true
	}
	for _, sub := range secretSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// allSecretKeyNames returns all exact key names and substring patterns
// as alternatives for the regex builder.
func allSecretKeyNames() []string {
	seen := map[string]bool{}
	var names []string
	for k := range secretQueryKeys {
		if !seen[k] {
			seen[k] = true
			names = append(names, regexp.QuoteMeta(k))
		}
	}
	for _, s := range secretSubstrings {
		p := regexp.QuoteMeta(s)
		if !seen[p] {
			seen[p] = true
			names = append(names, p)
		}
	}
	return names
}

// redactLibpqKeywordDSN redacts secret keys in libpq keyword=value DSNs.
// Returns (redacted, true) when input looks like keyword form; otherwise ("", false).
func redactLibpqKeywordDSN(dsn string) (string, bool) {
	if strings.Contains(dsn, "://") || !strings.Contains(dsn, "=") {
		return "", false
	}
	return libpqKeywordRE.ReplaceAllStringFunc(dsn, redactKeyValueMatch), true
}

// RedactMessage scrubs password-like query parameters from arbitrary text.
// It is meant for log/error messages that may contain embedded DSNs or
// connection strings. URL DSNs delegate to RedactDSN; libpq keyword pairs
// and embedded query params are scrubbed via regex.
func RedactMessage(s string) string {
	// Try whole-string URL first.
	if r := RedactDSN(s); r != s {
		return r
	}
	// Libpq keyword DSN as whole string.
	if redacted, ok := redactLibpqKeywordDSN(s); ok && redacted != s {
		return redacted
	}
	// Embedded PostgreSQL URLs may carry userinfo passwords.
	s = postgresURLRE.ReplaceAllStringFunc(s, RedactDSN)
	// Regex-based redaction for embedded query params in arbitrary text.
	s = redactParamRE.ReplaceAllStringFunc(s, redactKeyValueMatch)
	// Libpq keyword pairs embedded in error text (host=x password=secret).
	return libpqKeywordRE.ReplaceAllStringFunc(s, redactKeyValueMatch)
}

func redactKeyValueMatch(match string) string {
	eq := strings.IndexByte(match, '=')
	if eq < 0 {
		return match
	}
	prefix := ""
	keyStart := 0
	if match[0] == '&' || match[0] == '?' {
		prefix = string(match[0])
		keyStart = 1
	}
	key := match[keyStart:eq]
	return prefix + key + "=***"
}
