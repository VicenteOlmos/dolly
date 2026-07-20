package clone

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

// parsePostgresURL parses a PostgreSQL connection URL. It uses net/url when the
// input is already valid; otherwise it splits userinfo manually so passwords
// with raw special characters (%, ^, =, etc.) still work.
func parsePostgresURL(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err == nil {
		return u, nil
	}
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	return parsePostgresURLLenient(dsn)
}

func parsePostgresURLLenient(dsn string) (*url.URL, error) {
	var scheme, remainder string
	switch {
	case strings.HasPrefix(dsn, "postgres://"):
		scheme = "postgres"
		remainder = dsn[len("postgres://"):]
	case strings.HasPrefix(dsn, "postgresql://"):
		scheme = "postgresql"
		remainder = dsn[len("postgresql://"):]
	default:
		return nil, fmt.Errorf("parse DSN: unsupported scheme")
	}

	at := strings.LastIndex(remainder, "@")
	if at < 0 {
		u, err := url.Parse(scheme + "://" + remainder)
		if err != nil {
			return nil, fmt.Errorf("parse DSN: %w", err)
		}
		return u, nil
	}

	userinfo := remainder[:at]
	rest := remainder[at+1:]

	user, password, _ := strings.Cut(userinfo, ":")

	u, err := url.Parse(scheme + "://placeholder@" + rest)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	if password != "" {
		u.User = url.UserPassword(user, password)
	} else {
		u.User = url.User(user)
	}
	return u, nil
}

// RewriteDSN parses original as a URL, replaces the path (database name)
// with newDBName, and returns the rebuilt string preserving all query params.
func RewriteDSN(original, newDBName string) (string, error) {
	u, err := parsePostgresURL(original)
	if err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}
	u.Path = "/" + newDBName
	return u.String(), nil
}

// ParseDBName extracts the database name from a PostgreSQL DSN.
func ParseDBName(dsn string) (string, error) {
	u, err := parsePostgresURL(dsn)
	if err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return "", fmt.Errorf("DSN has no database name")
	}
	return dbName, nil
}

// SameInstance returns true if sourceDSN and targetDSN point to the same
// PostgreSQL host:port (or unix socket path). It normalises missing ports
// to the PostgreSQL default 5432.
func SameInstance(sourceDSN, targetDSN string) (bool, error) {
	sourceURL, err := parsePostgresURL(sourceDSN)
	if err != nil {
		return false, fmt.Errorf("parse source DSN: %w", err)
	}
	targetURL, err := parsePostgresURL(targetDSN)
	if err != nil {
		return false, fmt.Errorf("parse target DSN: %w", err)
	}

	sourceHost, sourcePort := hostPort(sourceURL)
	targetHost, targetPort := hostPort(targetURL)

	return sourceHost == targetHost && sourcePort == targetPort, nil
}

// DSNComponents holds connection fields extracted from a PostgreSQL DSN.
type DSNComponents struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// DecomposeDSN parses a PostgreSQL DSN into host, port, user, password, and database.
func DecomposeDSN(dsn string) (DSNComponents, error) {
	u, err := parsePostgresURL(dsn)
	if err != nil {
		return DSNComponents{}, err
	}
	dbName, err := ParseDBName(dsn)
	if err != nil {
		return DSNComponents{}, err
	}
	host, port := hostPort(u)
	var user, password string
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}
	return DSNComponents{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		Database: dbName,
	}, nil
}

// BuildPrimaryConninfo returns a libpq conninfo string for streaming replication.
func BuildPrimaryConninfo(c DSNComponents) string {
	parts := []string{
		conninfoKV("host", c.Host),
		conninfoKV("port", c.Port),
		conninfoKV("user", c.User),
		"application_name=dolly_clone",
	}
	if c.Password != "" {
		parts = append(parts, conninfoKV("password", c.Password))
	}
	return strings.Join(parts, " ")
}

func conninfoKV(key, value string) string {
	if value == "" {
		return key + "="
	}
	if strings.ContainsAny(value, " '\\") {
		return key + "='" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	return key + "=" + value
}

func hostPort(u *url.URL) (host, port string) {
	q := u.Query()
	if h := q.Get("host"); h != "" {
		host = h
	} else {
		host = u.Hostname()
	}
	if p := q.Get("port"); p != "" {
		port = p
	} else {
		port = u.Port()
	}
	if port == "" {
		port = "5432"
	}
	return
}

// StripPassword returns a subprocess-safe DSN and its PGPASSWORD value.
func StripPassword(dsn string) (cleanDSN, password string, err error) {
	return connections.SubprocessDSN(dsn)
}
