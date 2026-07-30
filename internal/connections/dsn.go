package connections

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var subprocessQueryControls = map[string]bool{
	"application_name": true, "channel_binding": true, "connect_timeout": true,
	"gssencmode": true, "gsslib": true, "host": true, "hostaddr": true,
	"keepalives": true, "keepalives_count": true, "keepalives_idle": true,
	"keepalives_interval": true, "krbsrvname": true, "load_balance_hosts": true,
	"port": true, "sslcert": true, "sslcrl": true, "sslcrldir": true, "sslkey": true,
	"ssl_max_protocol_version": true, "ssl_min_protocol_version": true,
	"sslmode": true, "sslnegotiation": true, "sslrootcert": true,
	"target_session_attrs": true, "tcp_user_timeout": true,
	"dbname": true, "database": true, "user": true,
}

// SubprocessDSN returns a PostgreSQL URL or keyword conninfo safe for command
// arguments and its password for PGPASSWORD. It never returns the original DSN
// on parse failure.
func SubprocessDSN(dsn string) (clean, password string, err error) {
	if strings.Contains(dsn, "://") {
		return subprocessURLDSN(dsn)
	}
	if strings.Contains(dsn, "=") {
		return subprocessKeywordDSN(dsn)
	}
	return "", "", errors.New("invalid PostgreSQL DSN")
}

func subprocessURLDSN(dsn string) (clean, password string, err error) {
	u, err := parsePostgresURL(dsn)
	if err != nil {
		return "", "", errors.New("invalid PostgreSQL DSN")
	}
	if u.User != nil {
		password, _ = u.User.Password()
		u.User = url.User(u.User.Username())
	}
	q := u.Query()
	if password == "" {
		password = q.Get("password")
	}
	for key := range q {
		if !subprocessQueryControls[key] {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), password, nil
}

func subprocessKeywordDSN(dsn string) (clean, password string, err error) {
	tokens, err := tokenizeKeywordDSN(dsn)
	if err != nil {
		return "", "", errors.New("invalid PostgreSQL DSN")
	}
	var b strings.Builder
	b.Grow(len(dsn))
	for _, tok := range tokens {
		lower := strings.ToLower(tok.key)
		switch lower {
		case "password":
			if password == "" {
				password = libpqValueBytes(tok.rawValue)
			}
			continue
		case "statement_timeout":
			continue
		}
		if !subprocessQueryControls[lower] {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(tok.key)
		b.WriteByte('=')
		b.WriteString(tok.rawValue)
	}
	if b.Len() == 0 {
		return "", "", errors.New("invalid PostgreSQL DSN")
	}
	return b.String(), password, nil
}

func libpqValueBytes(raw string) string {
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'")
	}
	return raw
}

func parsePostgresURL(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		return u, nil
	}
	var scheme string
	switch {
	case strings.HasPrefix(dsn, "postgres://"):
		scheme = "postgres"
	case strings.HasPrefix(dsn, "postgresql://"):
		scheme = "postgresql"
	default:
		return nil, errors.New("unsupported DSN")
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(dsn, "postgres://"), "postgresql://")
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return url.Parse(scheme + "://" + rest)
	}
	user, password, _ := strings.Cut(rest[:at], ":")
	u, err = url.Parse(scheme + "://placeholder@" + rest[at+1:])
	if err != nil {
		return nil, err
	}
	if password == "" {
		u.User = url.User(user)
	} else {
		u.User = url.UserPassword(user, password)
	}
	return u, nil
}

// Signature returns a stable key for host, port, database, and user.
func (c Connection) Signature() string {
	port := c.Port
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("%s|%s|%s|%s", c.Host, port, c.Database, c.User)
}

// DSN composes a PostgreSQL connection URL from profile fields.
func (c Connection) DSN() string {
	port := c.Port
	if port == "" {
		port = "5432"
	}
	sslmode := c.SSLMODE
	if sslmode == "" {
		sslmode = "verify-full"
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   net.JoinHostPort(c.Host, port),
		Path:   "/" + c.Database,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	if sslmode != "disable" {
		q.Set("channel_binding", "require")
	}
	u.RawQuery = q.Encode()
	return u.String()
}
