package connections

import (
	"errors"
	"net/url"
	"strings"
)

// ErrMalformedDSN is returned when SetDSNParam cannot classify the DSN.
// The message matches SubprocessDSN convention and must not embed DSN bytes.
var ErrMalformedDSN = errors.New("malformed PostgreSQL DSN")

// SetDSNParam injects key=value into a PostgreSQL DSN, overwriting any existing
// key (case-insensitive). URL DSNs use url.Parse; libpq keyword DSNs use a
// pure-string tokenizer honoring '…' quoting, '' doubling, and \ escapes.
// Disabled values ("" or "0") leave the DSN unchanged.
func SetDSNParam(dsn, key, value string) (string, error) {
	if key == "" {
		return "", ErrMalformedDSN
	}
	if value == "" || value == "0" {
		return dsn, nil
	}
	if strings.Contains(dsn, "://") {
		return setURLParam(dsn, key, value)
	}
	if strings.Contains(dsn, "=") {
		return setKeywordParam(dsn, key, value)
	}
	return "", ErrMalformedDSN
}

func setURLParam(dsn, key, value string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", ErrMalformedDSN
	}
	switch strings.ToLower(u.Scheme) {
	case "postgres", "postgresql":
	default:
		return "", ErrMalformedDSN
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

type keywordToken struct {
	key, rawValue string
}

func setKeywordParam(dsn, key, value string) (string, error) {
	tokens, err := tokenizeKeywordDSN(dsn)
	if err != nil {
		return "", ErrMalformedDSN
	}
	lowerKey := strings.ToLower(key)
	replaced := false
	var b strings.Builder
	b.Grow(len(dsn) + len(key) + len(value) + 4)
	for i, tok := range tokens {
		if i > 0 {
			b.WriteByte(' ')
		}
		if strings.ToLower(tok.key) == lowerKey {
			if !replaced {
				b.WriteString(tok.key)
				b.WriteByte('=')
				b.WriteString(value)
				replaced = true
			}
			continue
		}
		b.WriteString(tok.key)
		b.WriteByte('=')
		b.WriteString(tok.rawValue)
	}
	if !replaced {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(value)
	}
	return b.String(), nil
}

func tokenizeKeywordDSN(dsn string) ([]keywordToken, error) {
	var tokens []keywordToken
	for i, n := 0, len(dsn); i < n; {
		for i < n && dsn[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}
		eq, ok := findEqOutsideQuotes(dsn, i)
		if !ok || eq < 0 {
			return nil, ErrMalformedDSN
		}
		key := strings.TrimSpace(dsn[i:eq])
		if key == "" {
			return nil, ErrMalformedDSN
		}
		valEnd, ok := scanValue(dsn, eq+1)
		if !ok {
			return nil, ErrMalformedDSN
		}
		tokens = append(tokens, keywordToken{key, dsn[eq+1 : valEnd]})
		i = valEnd
	}
	if len(tokens) == 0 {
		return nil, ErrMalformedDSN
	}
	return tokens, nil
}

func advanceQuoted(s string, i int) (int, bool) {
	for i++; i < len(s); i++ {
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			return i, true
		}
	}
	return len(s), false
}

func findEqOutsideQuotes(s string, pos int) (int, bool) {
	for i := pos; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) {
				i++
			}
		case '\'':
			var ok bool
			i, ok = advanceQuoted(s, i)
			if !ok {
				return -1, false
			}
		case ' ':
			return -1, true
		case '=':
			return i, true
		}
	}
	return -1, true
}

func scanValue(s string, pos int) (int, bool) {
	for i := pos; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) {
				i++
			}
		case '\'':
			var ok bool
			i, ok = advanceQuoted(s, i)
			if !ok {
				return i, false
			}
		case ' ':
			return i, true
		}
	}
	return len(s), true
}
