package connections

import (
	"fmt"
	"net/url"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/joho/godotenv"
)

// ConnectionFromDotEnv loads a .env file at path and assembles a Connection
// profile from its PostgreSQL components. When path is empty the function
// returns config.ErrSourceDSNNotFound because an explicit .env path is
// required for profile mapping. If DB_URL contains an sslmode query parameter
// it is preserved in the resulting Connection.
func ConnectionFromDotEnv(path string, names config.EnvVarNames) (Connection, error) {
	if path == "" {
		return Connection{}, fmt.Errorf("%w: .env path required for profile mapping", config.ErrSourceDSNNotFound)
	}
	host, port, name, user, password, err := config.LoadDotEnvComponents(path, names)
	if err != nil {
		return Connection{}, err
	}

	// Extract sslmode from DB_URL query params when present.
	sslmode := ""
	if env, readErr := godotenv.Read(path); readErr == nil {
		if rawURL := env[names.URLVar]; rawURL != "" {
			if u, parseErr := url.Parse(rawURL); parseErr == nil {
				sslmode = u.Query().Get("sslmode")
			}
		}
	}

	return Connection{
		Name:     "local",
		Host:     host,
		Port:     port,
		Database: name,
		User:     user,
		Password: password,
		SSLMODE:  sslmode,
	}, nil
}
