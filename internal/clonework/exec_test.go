package clonework

import (
	"os"
	"strings"
	"testing"
)

func TestNoExecImport(t *testing.T) {
	src, err := os.ReadFile("run.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if strings.Contains(body, "os/exec") {
		t.Fatal("clonework must not import os/exec")
	}
}
