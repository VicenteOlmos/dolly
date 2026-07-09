package connections

import (
	"bytes"
	"strings"
	"testing"
)

func TestPickPromptByNumber(t *testing.T) {
	store := newTestStore(t)
	if err := store.Save(sampleConnection("staging")); err != nil {
		t.Fatal(err)
	}

	var w bytes.Buffer
	conn, err := PickPrompt(strings.NewReader("1\n"), &w, store)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Name != "staging" {
		t.Fatalf("got %q, want staging", conn.Name)
	}
}
