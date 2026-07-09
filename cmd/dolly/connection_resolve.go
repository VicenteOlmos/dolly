package main

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

// resolveLoadDotEnv is a test seam — overridable in tests.
var resolveLoadDotEnv = config.LoadDotEnv

func envNamesFromConfig(cfg *config.Config) config.EnvVarNames {
	return config.EnvVarNames{
		URLVar:      cfg.Env.URLVar,
		HostVar:     cfg.Env.HostVar,
		PortVar:     cfg.Env.PortVar,
		NameVar:     cfg.Env.NameVar,
		UserVar:     cfg.Env.UserVar,
		PasswordVar: cfg.Env.PasswordVar,
	}
}

func validateDSNOrConnection(connection, dsn string) error {
	if connection != "" && dsn != "" {
		return errors.New("--connection and --dsn are mutually exclusive")
	}
	return nil
}

func resolveDataSource(cfg *config.Config, cwd, connection, dsn string) (resolvedDSN string, schemas []string, err error) {
	if connection == "" && dsn == "" {
		envDSN, envErr := resolveLoadDotEnv(cfg.Env.Path, envNamesFromConfig(cfg))
		if envErr != nil {
			if errors.Is(envErr, config.ErrSourceDSNNotFound) {
				return "", nil, errors.New("no connection: provide --dsn, --connection, or a .env file with DB_HOST/DB_NAME/DB_USER (DB_URL also accepted)")
			}
			return "", nil, envErr
		}
		return envDSN, nil, nil
	}
	if connection == "" {
		return dsn, nil, nil
	}
	conn, err := connections.Resolve(cfg, cwd, connection)
	if err != nil {
		if errors.Is(err, connections.ErrNotFound) {
			return "", nil, fmt.Errorf("connection profile %q not found", connection)
		}
		return "", nil, err
	}
	return conn.DSN(), append([]string(nil), conn.Schemas...), nil
}

// appendQueryParam adds or overwrites a single query parameter in a PostgreSQL DSN URL.
func appendQueryParam(dsn, key, value string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		// ponytail: DSN can't be parsed — return as-is; timeout won't apply.
		return dsn
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}
