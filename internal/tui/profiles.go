package tui

import (
	"strings"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func parseCommaSchemas(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func draftFromConnection(c connections.Connection) ConnectionDraft {
	return ConnectionDraft{
		Host:     c.Host,
		Port:     c.Port,
		Database: c.Database,
		User:     c.User,
		Password: c.Password,
		SSLMODE:  c.SSLMODE,
	}
}

func connectionFromDraft(d ConnectionDraft, name string, schemas []string) connections.Connection {
	return connections.Connection{
		Name:     name,
		Host:     d.Host,
		Port:     d.Port,
		Database: d.Database,
		User:     d.User,
		Password: d.Password,
		SSLMODE:  d.SSLMODE,
		Schemas:  append([]string(nil), schemas...),
	}
}
