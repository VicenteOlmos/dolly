package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type readmeCase struct {
	lang            string
	file            string
	orderMarkers    []string
	envCreate       string
	fidelityPhrases []string
	recommendHints  []string
	forbiddenClaims []string
}

var readmeCases = []readmeCase{
	{
		lang: "en",
		file: "README.md",
		orderMarkers: []string{
			"## Install",
			"<!-- readme:quick-start-clone -->",
			"## Quick start: clone",
			"without changing",
			"<!-- situation-guidance:start -->",
			"## Common workflows and limits",
			"### Clone strategies",
		},
		envCreate: "create a `.env`",
		fidelityPhrases: []string{
			"schema-replay", "trigger", "materialized-view", "table data",
			"sequence", "not cloned", "may fire", "owners", "ACL", "cluster-global",
		},
		recommendHints:  []string{"recommend", "recommended"},
		forbiddenClaims: []string{"dolly requires", "dolly will change", "dolly changes", "dolly modifies"},
	},
	{
		lang: "es",
		file: "README.es.md",
		orderMarkers: []string{
			"## Instalación",
			"<!-- readme:quick-start-clone -->",
			"## Inicio rápido: clonar",
			"sin cambiar",
			"<!-- situation-guidance:start -->",
			"## Flujos de trabajo y límites habituales",
			"### Estrategias de clonación",
		},
		envCreate: "cree un archivo `.env`",
		fidelityPhrases: []string{
			"schema-replay", "disparador", "vistas materializadas", "datos de tablas",
			"secuencia", "no se clona", "pueden ejecutarse", "propietarios", "ACL", "ámbito de clúster",
		},
		recommendHints:  []string{"recomiend"},
		forbiddenClaims: []string{"dolly requiere", "dolly cambiará", "dolly cambia", "dolly modifica"},
	},
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func indexOrFail(t *testing.T, doc, needle, label string) int {
	t.Helper()
	i := strings.Index(doc, needle)
	if i < 0 {
		t.Fatalf("%s marker missing %q", label, needle)
	}
	return i
}

func extractBetween(t *testing.T, doc, start, end, label string) string {
	t.Helper()
	i := strings.Index(doc, start)
	if i < 0 {
		t.Fatalf("%s start marker missing %q", label, start)
	}
	j := strings.Index(doc[i:], end)
	if j < 0 {
		t.Fatalf("%s end marker missing %q", label, end)
	}
	return doc[i : i+j]
}

func TestREADMECloneFirstFlow(t *testing.T) {
	envNames := []string{"DB_URL", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD"}

	for _, tc := range readmeCases {
		t.Run(tc.lang, func(t *testing.T) {
			doc := readRepoFile(t, tc.file)

			prev := -1
			for _, m := range tc.orderMarkers {
				i := indexOrFail(t, doc, m, tc.lang)
				if i <= prev {
					t.Fatalf("%s: marker %q at %d not after previous at %d", tc.lang, m, i, prev)
				}
				prev = i
			}

			quick := extractBetween(t, doc, "<!-- readme:quick-start-clone -->", "<!-- situation-guidance:start -->", tc.lang)
			for _, marker := range append([]string{tc.envCreate, "dolly clone"}, envNames...) {
				if !strings.Contains(quick, marker) {
					t.Fatalf("%s quick start missing %q", tc.lang, marker)
				}
			}

			fidelity := extractBetween(t, doc, "<!-- readme:fidelity:schema-replay -->", "<!-- /readme:fidelity:schema-replay -->", tc.lang)
			lowerFidelity := strings.ToLower(fidelity)
			for _, phrase := range tc.fidelityPhrases {
				if !strings.Contains(lowerFidelity, strings.ToLower(phrase)) {
					t.Fatalf("%s fidelity block missing %q:\n%s", tc.lang, phrase, fidelity)
				}
			}

			security := extractBetween(t, doc, "<!-- readme:security:dotenv-advisory -->", "<!-- /readme:security:dotenv-advisory -->", tc.lang)
			lowerSecurity := strings.ToLower(security)
			if !containsAny(lowerSecurity, tc.recommendHints) {
				t.Fatalf("%s security block should recommend owner-only permissions", tc.lang)
			}
			for _, forbidden := range tc.forbiddenClaims {
				if strings.Contains(lowerSecurity, forbidden) {
					t.Fatalf("%s security block must not claim %q", tc.lang, forbidden)
				}
			}
		})
	}
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
