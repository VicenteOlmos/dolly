package connections

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestEncryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	t.Setenv(encryptEnvVar, base64.StdEncoding.EncodeToString(key))

	plain := []byte("connections:\n  - name: prod\n    host: h\n    database: d\n    user: u\n    password: secret\n")
	sealed, err := sealPlaintext(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !isCipherEnvelope(sealed) {
		t.Fatal("expected cipher envelope")
	}
	got, err := openCiphertext(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("round-trip mismatch:\n%s", got)
	}
}

func TestEncryptMissingKey(t *testing.T) {
	t.Setenv(encryptEnvVar, "")
	_, err := sealPlaintext([]byte("x"))
	if err == nil {
		t.Fatal("expected error without key")
	}
}

func TestFileStoreEncryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	t.Setenv(encryptEnvVar, base64.StdEncoding.EncodeToString(key))

	dir := t.TempDir()
	path := dir + "/.dolly.connections.yaml"
	store, err := NewFileStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(sampleConnection("prod")); err != nil {
		t.Fatal(err)
	}

	store2, err := NewFileStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store2.Get("prod")
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "secret" {
		t.Fatalf("password = %q", got.Password)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !isCipherEnvelope(data) {
		t.Fatal("expected encrypted file on disk")
	}
	if string(data) != "" && containsString(string(data), "password: secret") {
		t.Fatal("password must not appear in ciphertext file")
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && indexString(s, sub) >= 0
}

func indexString(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
