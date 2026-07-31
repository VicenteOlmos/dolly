package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

const broadDotEnvPermissionsWarning = "warning: dotenv permissions allow group or other access; continuing without changing the file"

// ErrSourceDSNNotFound is returned when neither DB_URL nor discrete DB_* vars resolve a DSN.
var ErrSourceDSNNotFound = errors.New("source DSN not found")

// EnvVarNames holds the names of environment variables used for DSN resolution.
type EnvVarNames struct {
	URLVar      string
	HostVar     string
	PortVar     string
	NameVar     string
	UserVar     string
	PasswordVar string
}

func readDotEnv(path string, warnings io.Writer) (map[string]string, error) {
	if path == "" {
		return nil, nil
	}
	broad, err := dotenvPermissionAdvisory(path)
	if err != nil {
		return nil, fmt.Errorf("tighten %s: %w", path, err)
	}
	if broad && warnings != nil {
		_, _ = io.WriteString(warnings, broadDotEnvPermissionsWarning+"\n")
	}
	env, err := godotenv.Read(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	if os.IsNotExist(err) {
		return nil, nil
	}
	return env, nil
}

// LoadDotEnvComponents reads a .env file (if present) and returns the discrete
// DB connection fields: host, port, name (database), user, and password.
// If URLVar is set it is parsed to extract the components; otherwise discrete
// vars are read. A missing .env file is non-fatal — shell env vars act as
// fallback. Returns ErrSourceDSNNotFound when host, name, and user are all
// empty after resolution.
func LoadDotEnvComponents(path string, names EnvVarNames) (host, port, name, user, password string, err error) {
	env, err := readDotEnv(path, os.Stderr)
	if err != nil {
		return "", "", "", "", "", err
	}

	get := func(key string) string {
		if v, ok := env[key]; ok {
			return v
		}
		return os.Getenv(key)
	}

	if u := get(names.URLVar); u != "" {
		parsed, parseErr := url.Parse(u)
		if parseErr != nil {
			return "", "", "", "", "", fmt.Errorf("parse %s: %w", names.URLVar, parseErr)
		}
		h := parsed.Hostname()
		p := parsed.Port()
		n := strings.TrimPrefix(parsed.Path, "/")
		usr := ""
		pwd := ""
		if parsed.User != nil {
			usr = parsed.User.Username()
			pwd, _ = parsed.User.Password()
		}
		if h == "" && n == "" && usr == "" {
			return "", "", "", "", "", fmt.Errorf("%w: set %s or %s/%s/%s/%s",
				ErrSourceDSNNotFound, names.URLVar, names.HostVar, names.NameVar, names.UserVar, names.PortVar)
		}
		return h, p, n, usr, pwd, nil
	}

	host = get(names.HostVar)
	port = get(names.PortVar)
	name = get(names.NameVar)
	user = get(names.UserVar)
	password = get(names.PasswordVar)

	if host == "" && name == "" && user == "" {
		return "", "", "", "", "", fmt.Errorf("%w: set %s or %s/%s/%s/%s",
			ErrSourceDSNNotFound, names.URLVar, names.HostVar, names.NameVar, names.UserVar, names.PortVar)
	}
	return host, port, name, user, password, nil
}

// LoadDotEnv reads a .env file (if present) and resolves a source DSN.
// It first checks the URL variable; if absent it assembles a DSN from discrete
// host/port/name/user/password variables.
// A missing .env file is treated as a no-op so that existing shell env vars can
// still be used.
func LoadDotEnv(path string, names EnvVarNames) (string, error) {
	env, err := readDotEnv(path, os.Stderr)
	if err != nil {
		return "", err
	}

	get := func(key string) string {
		if v, ok := env[key]; ok {
			return v
		}
		return os.Getenv(key)
	}

	if u := get(names.URLVar); u != "" {
		return u, nil
	}

	host := get(names.HostVar)
	port := get(names.PortVar)
	dbName := get(names.NameVar)
	user := get(names.UserVar)
	password := get(names.PasswordVar)

	if host == "" || dbName == "" || user == "" {
		return "", fmt.Errorf("%w: set %s or %s/%s/%s/%s",
			ErrSourceDSNNotFound, names.URLVar, names.HostVar, names.PortVar, names.NameVar, names.UserVar)
	}

	u := &url.URL{
		Scheme: "postgres",
		Host:   host,
		Path:   "/" + dbName,
	}
	if password != "" {
		u.User = url.UserPassword(user, password)
	} else {
		u.User = url.User(user)
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	return u.String(), nil
}
