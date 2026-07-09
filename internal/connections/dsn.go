package connections

import (
	"fmt"
	"net"
	"net/url"
)

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
