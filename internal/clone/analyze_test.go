package clone

import (
	"context"
	"testing"
)

func TestAnalyzeSourceSeamFunction(t *testing.T) {
	if analyzeSourceFunc == nil {
		t.Fatal("analyzeSourceFunc is nil")
	}
	_, err := analyzeSourceFunc(context.Background(), nil, "app", "{db}_dolly_{n}", nil)
	if err == nil {
		t.Fatal("expected error from analyzeSourceFunc with nil db")
	}
}
