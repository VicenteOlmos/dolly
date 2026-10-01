package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateLockTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr error
	}{
		{name: "empty", value: ""},
		{name: "zero", value: "0"},
		{name: "positive", value: "30s"},
		{name: "invalid parse", value: "not-a-duration", wantErr: ErrInvalidLockTimeout},
		{name: "non-positive", value: "-1s", wantErr: ErrInvalidLockTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.DB.LockTimeout = tt.value
			err := cfg.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateIdleInTransactionSessionTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DB.IdleInTransactionSessionTimeout = "bogus"
	if err := cfg.Validate(); !errors.Is(err, ErrInvalidIdleInTransactionSessionTimeout) {
		t.Fatalf("Validate() = %v, want %v", err, ErrInvalidIdleInTransactionSessionTimeout)
	}
}

func TestValidateApplicationName(t *testing.T) {
	for _, bad := range []string{"a=b", "a\r", "a\n"} {
		cfg := DefaultConfig()
		cfg.DB.ApplicationName = bad
		if err := cfg.Validate(); !errors.Is(err, ErrInvalidApplicationName) {
			t.Fatalf("application_name %q: Validate() = %v, want %v", bad, err, ErrInvalidApplicationName)
		}
	}
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default application_name: %v", err)
	}
}

func TestLoadConfigRejectsInvalidLockTimeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.jsonc")
	content := `{"db":{"lock_timeout":"-5s"}}`
	if err := writeTestConfig(p, content); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(p)
	if !errors.Is(err, ErrInvalidLockTimeout) {
		t.Fatalf("LoadConfig() = %v, want %v", err, ErrInvalidLockTimeout)
	}
}

func writeTestConfig(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
