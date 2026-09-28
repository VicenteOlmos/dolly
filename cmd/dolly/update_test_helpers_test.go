package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/update"
)

func buildTestArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	asset, err := update.CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}
	if strings.HasSuffix(asset, ".zip") {
		return buildTestZip(t, name, content)
	}
	return buildTestTarGz(t, name, content)
}

func buildTestTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildTestZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	hdr.SetMode(0o755)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildTestChecksum(t *testing.T, assetName string, archive []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(archive)
	line := hex.EncodeToString(sum[:]) + "  " + assetName + "\n"
	return []byte(line)
}
