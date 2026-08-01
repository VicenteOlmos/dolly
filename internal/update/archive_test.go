package update

import (
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	asset := "dolly_linux_x86_64.tar.gz"
	validHash := strings.Repeat("a", 64)
	otherHash := strings.Repeat("b", 64)
	valid := strings.Join([]string{
		otherHash + "  other.txt",
		validHash + "  " + asset,
	}, "\n")

	got, err := parseChecksums([]byte(valid), asset)
	if err != nil {
		t.Fatalf("parseChecksums: %v", err)
	}
	if got != validHash {
		t.Fatalf("got %q", got)
	}

	_, err = parseChecksums([]byte(valid+"\n"+valid), asset)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate err = %v", err)
	}

	short := strings.Repeat("c", 32)
	_, err = parseChecksums([]byte(short+"  "+asset), asset)
	if err == nil || !strings.Contains(err.Error(), "64 hex") {
		t.Fatalf("short hash err = %v", err)
	}

	badHex := strings.Repeat("z", 64)
	_, err = parseChecksums([]byte(badHex+"  "+asset), asset)
	if err == nil || !strings.Contains(err.Error(), "valid hex") {
		t.Fatalf("bad hex err = %v", err)
	}
}

func TestParseChecksumsMissingEntry(t *testing.T) {
	asset := "dolly_linux_x86_64.tar.gz"
	other := strings.Repeat("a", 64) + "  other.txt"
	_, err := parseChecksums([]byte(other), asset)
	if err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyArchiveSHA256Mismatch(t *testing.T) {
	err := verifyArchiveSHA256([]byte("data"), strings.Repeat("a", 64))
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err = %v", err)
	}
}
