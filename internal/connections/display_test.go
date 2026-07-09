package connections

import (
	"strings"
	"testing"
)

func TestDisplaySummaryMasksFieldsAndOmitsPassword(t *testing.T) {
	c := Connection{
		Name:     "staging",
		Host:     "db.example.com",
		User:     "app_user",
		Database: "production",
		Password: "super-secret",
	}
	got := DisplaySummary(c)
	if strings.Contains(got, "super-secret") {
		t.Fatalf("password leaked in summary: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Fatalf("expected masked fields in summary: %q", got)
	}
	if strings.Count(got, "/") != 2 {
		t.Fatalf("expected host/user/database segments: %q", got)
	}
}
