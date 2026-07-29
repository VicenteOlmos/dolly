package restore

import (
	"errors"
	"testing"
)

func TestEmptyDumpErrorIsSentinel(t *testing.T) {
	err := &EmptyDumpError{InputDir: "/tmp/empty"}
	if !errors.Is(err, ErrEmptyDump) {
		t.Fatal("expected ErrEmptyDump sentinel")
	}
	if !IsEmptyDumpError(err) {
		t.Fatal("expected IsEmptyDumpError")
	}
	if got := err.Error(); got != "dump contains no tables" {
		t.Fatalf("error = %q", got)
	}
}

func TestNoDataFilesErrorIsSentinel(t *testing.T) {
	err := &NoDataFilesError{InputDir: "/tmp/nodata", TableCount: 2}
	if !errors.Is(err, ErrNoDataFiles) {
		t.Fatal("expected ErrNoDataFiles sentinel")
	}
	if !IsNoDataFilesError(err) {
		t.Fatal("expected IsNoDataFilesError")
	}
	if got := err.Error(); got != "dump contains metadata for 2 table(s) but no data files" {
		t.Fatalf("error = %q", got)
	}
}
