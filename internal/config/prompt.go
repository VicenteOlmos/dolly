package config

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// PromptListSchemaNames lists non-system schema names on the source database.
// cmd/dolly sets this to cloneListSchemaNames before interactive clone.
var PromptListSchemaNames func(ctx context.Context, sourceDSN string) ([]string, error)

// RedactDSNFunc is an injectable DSN redaction function set by cmd/dolly to
// avoid an import cycle between config ↔ connections. When nil, DSN values
// are displayed as-is.
var RedactDSNFunc func(string) string

// PromptDefaults carries the pre-computed values shown to the user as defaults.
type PromptDefaults struct {
	SourceDSN string
	CloneName string
	TargetURL string // empty means "same host"
	Strategy  string
	Schemas   []string
}

// PromptResult captures the values chosen by the user (or accepted defaults).
type PromptResult struct {
	SourceDSN     string
	SourceSchemas []string
	CloneName     string
	TargetURL     string
	Strategy      string
}

// SavedSourcePicker offers saved-connection selection at the clone source prompt.
type SavedSourcePicker struct {
	Pick func(scanner *bufio.Scanner, w io.Writer) (dsn string, schemas []string, err error)
}

// IsStdinTerminal reports whether os.Stdin is a terminal.
func IsStdinTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// PromptSource runs interactive prompts for source, clone name, and target.
// When defaults.SourceDSN is empty (no .env / env vars), it asks for a source URL
// directly instead of offering a .env vs manual choice.
// When saved is non-nil, source mode also accepts "saved" to pick a stored profile.
func PromptSource(r io.Reader, w io.Writer, defaults PromptDefaults, saved *SavedSourcePicker) (PromptResult, error) {
	scanner := bufio.NewScanner(r)

	sourceDSN, sourceSchemas, err := promptSourceDSN(scanner, w, defaults, saved)
	if err != nil {
		return PromptResult{}, err
	}

	if len(sourceSchemas) == 0 && sourceDSN != "" {
		sourceSchemas, err = promptSchemas(context.Background(), scanner, w, sourceDSN, defaults.Schemas)
		if err != nil {
			return PromptResult{}, err
		}
	}

	fmt.Fprintf(w, "Clone name [%s]: ", defaults.CloneName)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return PromptResult{}, fmt.Errorf("prompt cancelled: %w", err)
		}
		return PromptResult{}, fmt.Errorf("prompt cancelled")
	}
	cloneName := strings.TrimSpace(scanner.Text())
	if cloneName == "" {
		cloneName = defaults.CloneName
	}

	targetDefault := "same"
	if defaults.TargetURL != "" {
		targetDefault = "custom"
	}
	fmt.Fprintf(w, "Target (same / custom, or paste postgres URL) [%s]: ", targetDefault)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return PromptResult{}, fmt.Errorf("prompt cancelled: %w", err)
		}
		return PromptResult{}, fmt.Errorf("prompt cancelled")
	}
	targetMode := strings.TrimSpace(scanner.Text())
	if targetMode == "" {
		targetMode = targetDefault
	}

	var targetURL string
	if looksLikePostgresDSN(targetMode) {
		// Users often paste the target URL here instead of typing "custom" first.
		targetURL = targetMode
	} else if targetMode == "custom" {
		fmt.Fprintf(w, "Enter target URL [%s]: ", redactOrEmpty(defaults.TargetURL))
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return PromptResult{}, fmt.Errorf("prompt cancelled: %w", err)
			}
			return PromptResult{}, fmt.Errorf("prompt cancelled")
		}
		targetURL = strings.TrimSpace(scanner.Text())
		if targetURL == "" {
			targetURL = defaults.TargetURL
		}
	} else {
		targetURL = defaults.TargetURL
	}

	stratDefault := defaults.Strategy
	if stratDefault == "" {
		stratDefault = "schema-replay"
	}
	fmt.Fprintf(w, "Strategy (template / schema-replay / logical-stream / physical-backup [cluster-level replica]) [%s]: ", stratDefault)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return PromptResult{}, fmt.Errorf("prompt cancelled: %w", err)
		}
		return PromptResult{}, fmt.Errorf("prompt cancelled")
	}
	strategy := strings.TrimSpace(scanner.Text())
	if strategy == "" {
		strategy = stratDefault
	}

	return PromptResult{
		SourceDSN:     sourceDSN,
		SourceSchemas: sourceSchemas,
		CloneName:     cloneName,
		TargetURL:     targetURL,
		Strategy:      strategy,
	}, nil
}

func promptSourceDSN(scanner *bufio.Scanner, w io.Writer, defaults PromptDefaults, saved *SavedSourcePicker) (string, []string, error) {
	if defaults.SourceDSN == "" {
		if saved != nil {
			return pickSavedOrManualURL(scanner, w, saved)
		}
		fmt.Fprintf(w, "Enter source URL (postgres://user:pass@host:5432/dbname): ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", nil, fmt.Errorf("prompt cancelled: %w", err)
			}
			return "", nil, fmt.Errorf("prompt cancelled")
		}
		sourceDSN := strings.TrimSpace(scanner.Text())
		if sourceDSN == "" {
			return "", nil, fmt.Errorf("source URL is required")
		}
		return sourceDSN, nil, nil
	}

	modePrompt := "Source mode (.env / manual"
	if saved != nil {
		modePrompt += " / saved"
	}
	modePrompt += ") [.env]: "
	fmt.Fprint(w, modePrompt)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", nil, fmt.Errorf("prompt cancelled: %w", err)
		}
		return "", nil, fmt.Errorf("prompt cancelled")
	}
	sourceMode := strings.TrimSpace(scanner.Text())
	if sourceMode == "" {
		sourceMode = ".env"
	}

	if sourceMode == "saved" {
		if saved == nil {
			return "", nil, fmt.Errorf("saved connections are not available")
		}
		return saved.Pick(scanner, w)
	}

	if sourceMode == "manual" {
		fmt.Fprintf(w, "Enter source URL: ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", nil, fmt.Errorf("prompt cancelled: %w", err)
			}
			return "", nil, fmt.Errorf("prompt cancelled")
		}
		sourceDSN := strings.TrimSpace(scanner.Text())
		if sourceDSN == "" {
			return "", nil, fmt.Errorf("source URL is required")
		}
		return sourceDSN, nil, nil
	}

	return defaults.SourceDSN, nil, nil
}

func pickSavedOrManualURL(scanner *bufio.Scanner, w io.Writer, saved *SavedSourcePicker) (string, []string, error) {
	modePrompt := "Source mode (manual"
	if saved != nil {
		modePrompt += " / saved"
	}
	modePrompt += ") [manual]: "
	fmt.Fprint(w, modePrompt)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", nil, fmt.Errorf("prompt cancelled: %w", err)
		}
		return "", nil, fmt.Errorf("prompt cancelled")
	}
	sourceMode := strings.TrimSpace(scanner.Text())
	if sourceMode == "" {
		sourceMode = "manual"
	}
	if sourceMode == "saved" {
		if saved == nil {
			return "", nil, fmt.Errorf("saved connections are not available")
		}
		return saved.Pick(scanner, w)
	}
	fmt.Fprintf(w, "Enter source URL (postgres://user:pass@host:5432/dbname): ")
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", nil, fmt.Errorf("prompt cancelled: %w", err)
		}
		return "", nil, fmt.Errorf("prompt cancelled")
	}
	sourceDSN := strings.TrimSpace(scanner.Text())
	if sourceDSN == "" {
		return "", nil, fmt.Errorf("source URL is required")
	}
	return sourceDSN, nil, nil
}

func looksLikePostgresDSN(s string) bool {
	return strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://")
}

// redactOrEmpty returns a redacted version of s via the injected
// RedactDSNFunc, or s unchanged when no function is set.
func redactOrEmpty(s string) string {
	if RedactDSNFunc == nil {
		return s
	}
	return RedactDSNFunc(s)
}

func promptSchemas(ctx context.Context, scanner *bufio.Scanner, w io.Writer, sourceDSN string, defaultSchemas []string) ([]string, error) {
	defaultLabel := strings.Join(defaultSchemas, ", ")
	if defaultLabel != "" {
		fmt.Fprintf(w, "Source schemas (comma-separated) [%s]: ", defaultLabel)
	} else {
		fmt.Fprint(w, "Source schemas (comma-separated): ")
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("prompt cancelled: %w", err)
		}
		return nil, fmt.Errorf("prompt cancelled")
	}

	var schemas []string
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		schemas = append([]string(nil), defaultSchemas...)
	} else {
		schemas = parseCommaSeparatedSchemas(line)
	}
	if len(schemas) == 0 {
		return nil, nil
	}

	if PromptListSchemaNames == nil {
		return nil, fmt.Errorf("schema catalog lister not configured")
	}
	catalog, err := PromptListSchemaNames(ctx, sourceDSN)
	if err != nil {
		return nil, fmt.Errorf("list source schemas: %w", err)
	}
	allowed := make(map[string]struct{}, len(catalog))
	for _, name := range catalog {
		allowed[name] = struct{}{}
	}
	for _, name := range schemas {
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("unknown schema %q (source has: %s)", name, strings.Join(catalog, ", "))
		}
	}
	return schemas, nil
}

func parseCommaSeparatedSchemas(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
