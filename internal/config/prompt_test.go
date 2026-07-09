package config

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func stubPromptListSchemaNames(t *testing.T, names []string, err error) {
	t.Helper()
	orig := PromptListSchemaNames
	PromptListSchemaNames = func(ctx context.Context, sourceDSN string) ([]string, error) {
		if err != nil {
			return nil, err
		}
		return append([]string(nil), names...), nil
	}
	t.Cleanup(func() { PromptListSchemaNames = orig })
}

func TestPromptSourceAcceptsDefaults(t *testing.T) {
	input := "\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		TargetURL: "",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SourceDSN != defaults.SourceDSN {
		t.Fatalf("expected SourceDSN=%q, got %q", defaults.SourceDSN, res.SourceDSN)
	}
	if res.CloneName != defaults.CloneName {
		t.Fatalf("expected CloneName=%q, got %q", defaults.CloneName, res.CloneName)
	}
	if res.TargetURL != defaults.TargetURL {
		t.Fatalf("expected TargetURL=%q, got %q", defaults.TargetURL, res.TargetURL)
	}
	if res.Strategy != defaults.Strategy {
		t.Fatalf("expected Strategy=%q, got %q", defaults.Strategy, res.Strategy)
	}

	output := w.String()
	if !strings.Contains(output, "Source mode") {
		t.Fatal("expected Source mode prompt")
	}
	if !strings.Contains(output, "Strategy") {
		t.Fatal("expected Strategy prompt")
	}
}

func TestPromptSourceManualURLOverride(t *testing.T) {
	input := "manual\npostgres://h-c/db\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		TargetURL: "",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SourceDSN != "postgres://h-c/db" {
		t.Fatalf("expected SourceDSN=postgres://h-c/db, got %q", res.SourceDSN)
	}
	if res.CloneName != defaults.CloneName {
		t.Fatalf("expected CloneName=%q, got %q", defaults.CloneName, res.CloneName)
	}
	if res.TargetURL != defaults.TargetURL {
		t.Fatalf("expected TargetURL=%q, got %q", defaults.TargetURL, res.TargetURL)
	}
	if res.Strategy != defaults.Strategy {
		t.Fatalf("expected Strategy=%q, got %q", defaults.Strategy, res.Strategy)
	}
}

func TestPromptSourceCustomCloneName(t *testing.T) {
	input := "\n\nmy_clone\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		TargetURL: "",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SourceDSN != defaults.SourceDSN {
		t.Fatalf("expected SourceDSN=%q, got %q", defaults.SourceDSN, res.SourceDSN)
	}
	if res.CloneName != "my_clone" {
		t.Fatalf("expected CloneName=my_clone, got %q", res.CloneName)
	}
	if res.TargetURL != defaults.TargetURL {
		t.Fatalf("expected TargetURL=%q, got %q", defaults.TargetURL, res.TargetURL)
	}
	if res.Strategy != defaults.Strategy {
		t.Fatalf("expected Strategy=%q, got %q", defaults.Strategy, res.Strategy)
	}
}

func TestPromptSourceCustomTargetURL(t *testing.T) {
	input := "\n\n\ncustom\npostgres://h-c/db\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		TargetURL: "",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SourceDSN != defaults.SourceDSN {
		t.Fatalf("expected SourceDSN=%q, got %q", defaults.SourceDSN, res.SourceDSN)
	}
	if res.CloneName != defaults.CloneName {
		t.Fatalf("expected CloneName=%q, got %q", defaults.CloneName, res.CloneName)
	}
	if res.TargetURL != "postgres://h-c/db" {
		t.Fatalf("expected TargetURL=postgres://h-c/db, got %q", res.TargetURL)
	}
	if res.Strategy != defaults.Strategy {
		t.Fatalf("expected Strategy=%q, got %q", defaults.Strategy, res.Strategy)
	}
}

func TestPromptSourceNoEnvAsksURL(t *testing.T) {
	input := "postgres://u:p@h-a:5432/db_a\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "",
		CloneName: "db_dolly_{n}",
		TargetURL: "",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SourceDSN != "postgres://u:p@h-a:5432/db_a" {
		t.Fatalf("expected manual URL, got %q", res.SourceDSN)
	}
	output := w.String()
	if strings.Contains(output, "Source mode") {
		t.Fatal("expected no Source mode prompt when .env is unavailable")
	}
	if !strings.Contains(output, "Enter source URL") {
		t.Fatal("expected direct source URL prompt")
	}
}

func TestPromptSourceNoEnvRequiresURL(t *testing.T) {
	input := "\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "",
		CloneName: "db_dolly_{n}",
		TargetURL: "",
		Strategy:  "schema-replay",
	}

	_, err := PromptSource(r, &w, defaults, nil)
	if err == nil {
		t.Fatal("expected error for empty source URL")
	}
	if !strings.Contains(err.Error(), "source URL is required") {
		t.Fatalf("error = %q, want source URL required", err.Error())
	}
}

func TestPromptSourceTargetURLAtModePrompt(t *testing.T) {
	const targetDSN = "postgres://u:p@target-host:5432/target_db"
	input := "\n\n\n" + targetDSN + "\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://reader:pass@source-host:5432/db_src",
		CloneName: "db_clone",
		TargetURL: "",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TargetURL != targetDSN {
		t.Fatalf("expected pasted target URL, got %q", res.TargetURL)
	}
	output := w.String()
	if strings.Contains(output, "Enter target URL") {
		t.Fatal("should not ask for target URL when user pasted DSN at mode prompt")
	}
}

func TestPromptSourceSavedMode(t *testing.T) {
	defaults := PromptDefaults{
		SourceDSN: "postgres://from-env/db",
		CloneName: "db_dolly_{n}",
		Strategy:  "schema-replay",
	}
	saved := &SavedSourcePicker{
		Pick: func(_ *bufio.Scanner, _ io.Writer) (string, []string, error) {
			return "postgres://saved/db", []string{"app", "billing"}, nil
		},
	}
	var buf strings.Builder
	res, err := PromptSource(strings.NewReader("saved\n1\n\n\n\n"), &buf, defaults, saved)
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceDSN != "postgres://saved/db" {
		t.Fatalf("SourceDSN = %q", res.SourceDSN)
	}
	if len(res.SourceSchemas) != 2 {
		t.Fatalf("SourceSchemas = %v", res.SourceSchemas)
	}
	if res.Strategy != defaults.Strategy {
		t.Fatalf("expected Strategy=%q, got %q", defaults.Strategy, res.Strategy)
	}
}

func TestPromptSourceCustomTargetDefault(t *testing.T) {
	input := "\n\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		TargetURL: "postgres://h-c/db",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TargetURL != defaults.TargetURL {
		t.Fatalf("expected TargetURL=%q, got %q", defaults.TargetURL, res.TargetURL)
	}
	if res.Strategy != defaults.Strategy {
		t.Fatalf("expected Strategy=%q, got %q", defaults.Strategy, res.Strategy)
	}
}

func TestPromptSourceCustomStrategy(t *testing.T) {
	input := "\n\n\n\ntemplate\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		TargetURL: "",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Strategy != "template" {
		t.Fatalf("expected Strategy=template, got %q", res.Strategy)
	}
}

func TestPromptSourceStrategyDefaultEmptyFallsBackToSchemaReplay(t *testing.T) {
	input := "\n\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		TargetURL: "",
		Strategy:  "",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Strategy != "schema-replay" {
		t.Fatalf("expected Strategy=schema-replay, got %q", res.Strategy)
	}
}

func TestPromptSchemasValidList(t *testing.T) {
	stubPromptListSchemaNames(t, []string{"app", "billing"}, nil)

	input := "\napp, billing\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer
	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.SourceSchemas) != 2 || res.SourceSchemas[0] != "app" || res.SourceSchemas[1] != "billing" {
		t.Fatalf("SourceSchemas = %v", res.SourceSchemas)
	}
	if !strings.Contains(w.String(), "Source schemas") {
		t.Fatal("expected schema prompt")
	}
}

func TestPromptSchemasUnknownNameError(t *testing.T) {
	stubPromptListSchemaNames(t, []string{"app", "billing"}, nil)

	input := "\nunknown\n"
	r := strings.NewReader(input)
	var w bytes.Buffer
	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		Strategy:  "schema-replay",
	}

	_, err := PromptSource(r, &w, defaults, nil)
	if err == nil {
		t.Fatal("expected error for unknown schema")
	}
	if !strings.Contains(err.Error(), `unknown schema "unknown"`) {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestPromptSchemasOverrideConfigDefault(t *testing.T) {
	stubPromptListSchemaNames(t, []string{"app", "billing"}, nil)

	input := "\nbilling\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer
	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		Strategy:  "schema-replay",
		Schemas:   []string{"app"},
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.SourceSchemas) != 1 || res.SourceSchemas[0] != "billing" {
		t.Fatalf("SourceSchemas = %v, want [billing]", res.SourceSchemas)
	}
}

func TestPromptSchemasDefaultFromConfig(t *testing.T) {
	stubPromptListSchemaNames(t, []string{"app", "billing"}, nil)

	input := "\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer
	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_a_dolly_{n}",
		Strategy:  "schema-replay",
		Schemas:   []string{"app"},
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.SourceSchemas) != 1 || res.SourceSchemas[0] != "app" {
		t.Fatalf("SourceSchemas = %v, want [app]", res.SourceSchemas)
	}
}

func TestPromptSourceSavedSkipsSchemaPrompt(t *testing.T) {
	defaults := PromptDefaults{
		SourceDSN: "postgres://from-env/db",
		CloneName: "db_dolly_{n}",
		Strategy:  "schema-replay",
	}
	saved := &SavedSourcePicker{
		Pick: func(_ *bufio.Scanner, _ io.Writer) (string, []string, error) {
			return "postgres://saved/db", []string{"app", "billing"}, nil
		},
	}
	var buf strings.Builder
	res, err := PromptSource(strings.NewReader("saved\n1\n\n\n\n"), &buf, defaults, saved)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SourceSchemas) != 2 {
		t.Fatalf("SourceSchemas = %v", res.SourceSchemas)
	}
	if strings.Contains(buf.String(), "Source schemas") {
		t.Fatal("saved profile with schemas should skip schema prompt")
	}
}

func TestPromptSourceRedactsTargetURLDefault(t *testing.T) {
	// Simulate CLI-layer redaction: defaults.TargetURL is pre-redacted
	// before reaching PromptSource. This test proves that a pre-redacted
	// URL does not leak its password in the prompt output.
	input := "\n\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_clone",
		TargetURL: "postgres://u:***@h-b:5432/db?sslmode=prefer",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TargetURL != defaults.TargetURL {
		t.Fatalf("TargetURL = %q, want %q", res.TargetURL, defaults.TargetURL)
	}

	output := w.String()
	if strings.Contains(output, "secret") {
		t.Fatalf("prompt output should not contain password:\n%s", output)
	}
	// Diagnostics (host, db) should still be present.
	if !strings.Contains(output, "h-b") || !strings.Contains(output, "db") {
		t.Fatalf("prompt output should preserve host and db diagnostics:\n%s", output)
	}
}

func TestPromptSourceRedactsSourceDSNDefault(t *testing.T) {
	// Defense-in-depth: when a password is in the source DSN, no prompt
	// path should echo it. With pre-redacted defaults, the password
	// should be absent from all output.
	input := "\n\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	redactedSource := "postgres://u:***@h-a:5432/db_src?sslmode=prefer"
	defaults := PromptDefaults{
		SourceDSN: redactedSource,
		CloneName: "db_clone",
		TargetURL: "postgres://u:***@h-c/db",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SourceDSN != redactedSource {
		t.Fatalf("SourceDSN = %q, want %q", res.SourceDSN, redactedSource)
	}

	output := w.String()
	if strings.Contains(output, "secret") {
		t.Fatalf("prompt output should not contain source password:\n%s", output)
	}
}

func TestPromptSourceCustomTargetRedaction(t *testing.T) {
	// User chooses "custom" target — default is shown redacted, user
	// types their own unredacted URL.
	input := "\n\n\ncustom\npostgres://u:secret@h-d:5432/db\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	defaults := PromptDefaults{
		SourceDSN: "postgres://u:p@h-a/db_src",
		CloneName: "db_clone",
		TargetURL: "postgres://u:***@h-b:5432/db_cfg",
		Strategy:  "schema-replay",
	}

	res, err := PromptSource(r, &w, defaults, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TargetURL != "postgres://u:secret@h-d:5432/db" {
		t.Fatalf("TargetURL = %q, want user-provided URL", res.TargetURL)
	}

	output := w.String()
	if strings.Contains(output, "secret") && !strings.Contains(output, "h-d") {
		// The user typed "secret" — that should appear in the
		// prompt echo but not in the default placeholder.
		t.Fatalf("prompt output should only contain user-typed secret, not leaked default:\n%s", output)
	}
}
