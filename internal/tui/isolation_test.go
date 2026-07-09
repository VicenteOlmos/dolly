package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForbiddenPackageImports(t *testing.T) {
	forbidden := []string{
		"github.com/VicenteOlmos/dolly/internal/dump",
		"github.com/VicenteOlmos/dolly/internal/restore",
		"github.com/VicenteOlmos/dolly/internal/clone",
	}

	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		if path == "dump_run.go" || path == "restore_run.go" {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			pathVal := strings.Trim(imp.Path.Value, `"`)
			for _, pkg := range forbidden {
				if pathVal == pkg {
					t.Fatalf("%s imports %s; TUI must not import restore/clone/dump packages except dump_run.go (dump only)", path, pkg)
				}
			}
		}
	}
}

func TestCloneRunDoesNotImportRestoreOrDump(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "clone_run.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse clone_run.go: %v", err)
	}
	for _, imp := range f.Imports {
		pathVal := strings.Trim(imp.Path.Value, `"`)
		switch pathVal {
		case "github.com/VicenteOlmos/dolly/internal/restore":
			t.Fatal("clone_run.go imports internal/restore; TUI clone must not import restore")
		case "github.com/VicenteOlmos/dolly/internal/dump":
			t.Fatal("clone_run.go imports internal/dump; TUI clone must use clone layer only")
		case "os/exec":
			t.Fatal("clone_run.go imports os/exec; TUI clone must not start subprocesses")
		}
	}
}

func TestCloneRunDoesNotReachCloneRunOrExec(t *testing.T) {
	src, err := os.ReadFile("clone_run.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if strings.Contains(body, "clone.Run") {
		t.Fatal("clone_run.go references clone.Run; production TUI clone must not invoke subprocess-backed clone")
	}
	if strings.Contains(body, "exec.Command") || strings.Contains(body, "exec.CommandContext") {
		t.Fatal("clone_run.go references exec.Command; TUI clone must not start subprocesses")
	}
}

func TestProductionCloneRunnerUsesClonework(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "clone_run.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse clone_run.go: %v", err)
	}
	var hasClonework bool
	for _, imp := range f.Imports {
		pathVal := strings.Trim(imp.Path.Value, `"`)
		switch pathVal {
		case "github.com/VicenteOlmos/dolly/internal/clonework":
			hasClonework = true
		case "github.com/VicenteOlmos/dolly/internal/clone", "github.com/VicenteOlmos/dolly/internal/dump", "github.com/VicenteOlmos/dolly/internal/restore", "os/exec":
			t.Fatalf("clone_run.go imports %s; production TUI clone must use clonework boundary only", pathVal)
		}
	}
	if !hasClonework {
		t.Fatal("clone_run.go must import dolly/internal/clonework for production clone")
	}

	src, err := os.ReadFile("clone_run.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if strings.Contains(body, "errTUICloneUnavailable") {
		t.Fatal("clone_run.go still gates production clone as unavailable")
	}
	if !strings.Contains(body, "clonework.Run") {
		t.Fatal("clone_run.go must call clonework.Run for production clone")
	}
}

func TestNoDumpPackageSelectorOutsideDumpRun(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, path := range matches {
		if path == "dump_run.go" || strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "dump" {
					t.Fatalf("%s references dump package selector", path)
				}
			}
			return true
		})
	}
}

func TestNoClonePackageSelectorOutsideCloneRun(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "clone" {
					t.Fatalf("%s references clone package selector", path)
				}
			}
			return true
		})
	}
}
