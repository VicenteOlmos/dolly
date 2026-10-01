package config

import (
	"strconv"
	"time"
)

// DSNParamSetter injects or overwrites a single query/keyword parameter in a PostgreSQL DSN.
type DSNParamSetter func(dsn, key, value string) (string, error)

// sessionGUCTimeoutForDSN converts a Go duration from config into PostgreSQL timeout
// milliseconds. Values that are not Go durations (e.g. statement_timeout "5min") pass through.
func sessionGUCTimeoutForDSN(raw string) string {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return raw
	}
	return strconv.FormatInt(d.Milliseconds(), 10)
}

// ApplySessionGUCs adds db session parameters from config to dsn when enabled.
func (c *Config) ApplySessionGUCs(dsn string, set DSNParamSetter) (string, error) {
	if set == nil {
		return dsn, nil
	}
	var err error
	if c.DB.StatementTimeout != "" && c.DB.StatementTimeout != "0" {
		dsn, err = set(dsn, "statement_timeout", sessionGUCTimeoutForDSN(c.DB.StatementTimeout))
		if err != nil {
			return "", err
		}
	}
	if c.DB.LockTimeout != "" && c.DB.LockTimeout != "0" {
		dsn, err = set(dsn, "lock_timeout", sessionGUCTimeoutForDSN(c.DB.LockTimeout))
		if err != nil {
			return "", err
		}
	}
	if c.DB.IdleInTransactionSessionTimeout != "" && c.DB.IdleInTransactionSessionTimeout != "0" {
		dsn, err = set(dsn, "idle_in_transaction_session_timeout", sessionGUCTimeoutForDSN(c.DB.IdleInTransactionSessionTimeout))
		if err != nil {
			return "", err
		}
	}
	if c.DB.ApplicationName != "" {
		dsn, err = set(dsn, "application_name", c.DB.ApplicationName)
		if err != nil {
			return "", err
		}
	}
	return dsn, nil
}
