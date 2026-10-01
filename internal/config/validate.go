package config

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidLockTimeout is returned when db.lock_timeout is not empty, "0", or a positive Go duration.
	ErrInvalidLockTimeout = errors.New("invalid db.lock_timeout")
	// ErrInvalidIdleInTransactionSessionTimeout is returned when db.idle_in_transaction_session_timeout is invalid.
	ErrInvalidIdleInTransactionSessionTimeout = errors.New("invalid db.idle_in_transaction_session_timeout")
	// ErrInvalidApplicationName is returned when db.application_name contains CR, LF, or '='.
	ErrInvalidApplicationName = errors.New("invalid db.application_name")
)

// Validate checks config fields that require constraints beyond JSON typing.
func (c *Config) Validate() error {
	if err := validateOptionalPositiveDuration(c.DB.LockTimeout, ErrInvalidLockTimeout); err != nil {
		return err
	}
	if err := validateOptionalPositiveDuration(c.DB.IdleInTransactionSessionTimeout, ErrInvalidIdleInTransactionSessionTimeout); err != nil {
		return err
	}
	if err := validateApplicationName(c.DB.ApplicationName); err != nil {
		return err
	}
	return nil
}

func validateOptionalPositiveDuration(raw string, sentinel error) error {
	if raw == "" || raw == "0" {
		return nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return sentinel
	}
	return nil
}

func validateApplicationName(name string) error {
	if name == "" {
		return nil
	}
	if strings.ContainsAny(name, "\r\n=") {
		return ErrInvalidApplicationName
	}
	return nil
}
