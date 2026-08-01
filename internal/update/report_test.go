package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConsumePendingReport(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dolly")
	if err := writePendingReport(dir, pendingReport{
		Status:        StatusUpdated,
		RemoteVersion: "v0.3.2",
	}); err != nil {
		t.Fatal(err)
	}

	result, consumed := consumePendingReport(dir, target, "v0.3.1")
	if !consumed {
		t.Fatal("expected consumed report")
	}
	if result.Status != StatusUpdated || result.RemoteVersion != "v0.3.2" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(reportPath(dir)); !os.IsNotExist(err) {
		t.Fatal("report should be removed after consume")
	}
}
