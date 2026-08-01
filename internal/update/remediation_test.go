package update

import (
	"context"
	"strings"
	"testing"
)

func TestRunRejectsChecksumMismatchWithoutMutation(t *testing.T) {
	assetName, err := CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("#!/bin/sh\necho newer\n")
	archive := buildCurrentArchive(t, content)
	wrongChecksums := []byte(checksumLine(assetName, []byte("wrong-archive")))

	target := writeFakeBinary(t, t.TempDir(), "dolly", 0o755)
	before := fileSHA256(t, target)

	result, err := Run(context.Background(), Options{
		HTTP:             mockReleaseClient(t, assetName, archive, wrongChecksums, "v0.3.2"),
		InstalledVersion: "0.3.1",
		TargetPath:       target,
	})
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err = %v", err)
	}
	if result == nil || result.Status != StatusFailed {
		t.Fatalf("result = %+v", result)
	}
	if after := fileSHA256(t, target); after != before {
		t.Fatal("target mutated on checksum mismatch")
	}
}
