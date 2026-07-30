package clone

import (
	"fmt"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

// TLSParams returns libpq TLS environment variables derived from sourceDSN.
func TLSParams(sourceDSN string) (map[string]string, error) {
	clean, _, err := connections.SubprocessDSN(sourceDSN)
	if err != nil {
		return nil, fmt.Errorf("parse source DSN: %w", err)
	}
	u, err := parsePostgresURL(clean)
	if err != nil {
		return nil, fmt.Errorf("parse source DSN: %w", err)
	}
	q := u.Query()
	env := map[string]string{}
	if v := q.Get("sslmode"); v != "" {
		env["PGSSLMODE"] = v
	}
	if v := q.Get("sslrootcert"); v != "" {
		env["PGSSLROOTCERT"] = v
	}
	if v := q.Get("channel_binding"); v != "" {
		env["PGCHANNELBINDING"] = v
	}
	return env, nil
}
