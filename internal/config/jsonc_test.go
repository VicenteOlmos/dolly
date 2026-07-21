package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStripJSONC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, out []byte)
	}{
		{
			name:  "plain JSON passthrough",
			input: `{"a":1,"b":"hello"}`,
			check: validJSON,
		},
		{
			name:  "line comment removed",
			input: "{\n// this is a comment\n\"a\":1\n}",
			check: func(t *testing.T, out []byte) {
				t.Helper()
				validJSON(t, out)
				var m map[string]int
				if err := json.Unmarshal(out, &m); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if m["a"] != 1 {
					t.Fatalf("a = %d, want 1", m["a"])
				}
			},
		},
		{
			name:  "inline line comment removed",
			input: `{"a":1 // comment` + "\n}",
			check: validJSON,
		},
		{
			name:  "block comment removed",
			input: `{"a": /* remove this */ 1}`,
			check: func(t *testing.T, out []byte) {
				t.Helper()
				validJSON(t, out)
				var m map[string]int
				if err := json.Unmarshal(out, &m); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if m["a"] != 1 {
					t.Fatalf("a = %d, want 1", m["a"])
				}
			},
		},
		{
			name:  "multi-line block comment",
			input: "{\n/* line one\n   line two\n*/\n\"x\":2\n}",
			check: func(t *testing.T, out []byte) {
				t.Helper()
				var m map[string]int
				if err := json.Unmarshal(out, &m); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if m["x"] != 2 {
					t.Fatalf("x = %d, want 2", m["x"])
				}
			},
		},
		{
			name:  "trailing comma before }",
			input: `{"a":1,}`,
			check: validJSON,
		},
		{
			name:  "trailing comma before ]",
			input: `{"arr":[1,2,3,]}`,
			check: validJSON,
		},
		{
			name:  "// inside string preserved",
			input: `{"url":"https://example.com//path"}`,
			check: func(t *testing.T, out []byte) {
				t.Helper()
				var m map[string]string
				if err := json.Unmarshal(out, &m); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if m["url"] != "https://example.com//path" {
					t.Fatalf("url = %q, want //path preserved", m["url"])
				}
			},
		},
		{
			name:  "/* inside string preserved",
			input: `{"re":"a /* b */"}`,
			check: func(t *testing.T, out []byte) {
				t.Helper()
				var m map[string]string
				if err := json.Unmarshal(out, &m); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if m["re"] != "a /* b */" {
					t.Fatalf("re = %q, want block comment preserved", m["re"])
				}
			},
		},
		{
			name:  "escape sequence inside string",
			input: `{"s":"line\\n\"quoted\""}`,
			check: func(t *testing.T, out []byte) {
				t.Helper()
				validJSON(t, out)
				var m map[string]string
				if err := json.Unmarshal(out, &m); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if m["s"] == "" {
					t.Fatal("s should not be empty")
				}
			},
		},
		{
			name:  "empty input",
			input: "",
			check: func(t *testing.T, out []byte) {
				t.Helper()
				if len(out) != 0 {
					t.Fatalf("expected empty output, got %q", out)
				}
			},
		},
		{
			name: "full JSONC example",
			input: `{
  // top-level comment
  "env": {
    "path": ".env", // inline comment
    "url_var": "DB_URL", /* block */
  },
  "clone": {
    "strategy": "schema-replay", // trailing comma + comment
  }
}`,
			check: func(t *testing.T, out []byte) {
				t.Helper()
				var m map[string]interface{}
				if err := json.Unmarshal(out, &m); err != nil {
					t.Fatalf("unmarshal: %v\noutput: %s", err, out)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := stripJSONC([]byte(tt.input))
			tt.check(t, out)
		})
	}
}

func validJSON(t *testing.T, out []byte) {
	t.Helper()
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON:\n%s", out)
	}
}

func TestWriteConfigFileRenameFailurePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	const wantMode = 0o640
	sentinel := errors.New("rename failed")
	original := configRename
	configRename = func(_, _ string) error { return sentinel }
	t.Cleanup(func() { configRename = original })

	err := WriteConfigFile(path, []byte("new"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("WriteConfigFile error = %v, want sentinel", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "old" {
		t.Fatalf("destination = %q, %v; want old, nil", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != wantMode {
		t.Fatalf("destination mode = %v, %v; want %o, nil", info.Mode(), err, wantMode)
	}
	staged, err := filepath.Glob(filepath.Join(dir, ".config.jsonc.tmp-*"))
	if err != nil || len(staged) != 0 {
		t.Fatalf("staged files = %v, %v; want none, nil", staged, err)
	}
}
