package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormatAnalyzeModalBody(t *testing.T) {
	r := analyzeResult{
		TableCount:       2,
		DatabaseSize:     128 * 1024 * 1024,
		NextCloneName:    "db_stub_dolly_1",
		TotalRowEstimate: 51200,
		Objects: []ObjectStat{
			{Schema: "public", Name: "users", Kind: "table", RowEstimate: 1200, SizeBytes: 1024 * 1024},
			{Schema: "app", Name: "orders", Kind: "table", RowEstimate: 50000, SizeBytes: 8 * 1024 * 1024},
		},
	}
	body := formatAnalyzeModalBody(r, 0)
	for _, want := range []string{
		"public.users",
		"app.orders",
		"Suggested clone name: db_stub_dolly_1",
		"51,200",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
}

func TestAnalyzeModalScroll(t *testing.T) {
	app := cloneAppWithSession(t, mockCloneRunner{})
	r := analyzeResult{
		Objects: make([]ObjectStat, 15),
	}
	for i := range r.Objects {
		r.Objects[i] = ObjectStat{
			Schema: "public",
			Name:   fmt.Sprintf("t%d", i+1),
			Kind:   "table",
		}
	}
	app.mountAnalyzeResultModal(r)
	if app.modal.scroll != 0 {
		t.Fatalf("scroll = %d, want 0", app.modal.scroll)
	}
	app = drainUpdate(app, keyPress("j", 'j', 0))
	if app.modal.scroll != 1 {
		t.Fatalf("scroll = %d, want 1 after j", app.modal.scroll)
	}
}
