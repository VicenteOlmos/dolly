package clone

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// cloneNamePattern matches valid clone names: alphanumeric and underscore
// only. PostgreSQL identifiers also permit $ and dot/dash, but Dolly rejects
// those to block injection paths. Template braces ({n}) are NOT accepted by
// the validator; ResolveTemplateName must be called first to replace them
// with concrete values.
//
// Spec: dolly-cli — Clone input validation — Unsafe clone name.
var cloneNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ValidateCloneName returns an error if name is empty or contains characters
// outside the safe set [a-zA-Z0-9_].
func ValidateCloneName(name string) error {
	if name == "" {
		return fmt.Errorf("clone name is required")
	}
	if !cloneNamePattern.MatchString(name) {
		return fmt.Errorf("clone name must contain only letters, digits, and underscores")
	}
	return nil
}

// ResolveTemplateName replaces exactly one "{n}" placeholder in template
// with the given integer n. If the template contains no placeholder,
// multiple placeholders, unclosed braces, or non-"{n}" brace patterns,
// it returns an error. The resolved name must still pass ValidateCloneName
// before execution.
//
// Spec: dolly-cli — Clone input validation — Template placeholders must be
// resolved before validation; raw braces must not reach runtime.
func ResolveTemplateName(template string, n int) (string, error) {
	if template == "" {
		return "", fmt.Errorf("template name is required")
	}

	// Detect unsupported brace patterns before checking for {n}.
	if strings.Contains(template, "{") || strings.Contains(template, "}") {
		if !strings.Contains(template, "{n}") {
			return "", fmt.Errorf("template contains unsupported brace pattern: %q", template)
		}
	}

	placeholder := "{n}"
	count := strings.Count(template, placeholder)
	if count == 0 {
		return "", fmt.Errorf("template missing {n} placeholder: %q", template)
	}
	if count > 1 {
		return "", fmt.Errorf("template must contain exactly one {n} placeholder: %q", template)
	}

	result := strings.ReplaceAll(template, placeholder, strconv.Itoa(n))

	// Reject any remaining braces — they are unsafe or invalid.
	if strings.Contains(result, "{") || strings.Contains(result, "}") {
		return "", fmt.Errorf("template contains unsupported brace pattern: %q", template)
	}

	return result, nil
}
