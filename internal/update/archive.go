package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxExpandedCandidate = 128 << 20 // 128 MiB

func parseChecksums(data []byte, assetName string) (string, error) {
	var matches []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		name := fields[len(fields)-1]
		if name != assetName {
			continue
		}
		if len(hash) != 64 {
			return "", fmt.Errorf("checksum for %q is not 64 hex characters", assetName)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return "", fmt.Errorf("checksum for %q is not valid hex", assetName)
		}
		matches = append(matches, hash)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("checksums.txt has no entry for %q", assetName)
	case 1:
		return strings.ToLower(matches[0]), nil
	default:
		return "", fmt.Errorf("checksums.txt has duplicate entries for %q", assetName)
	}
}

func verifyArchiveSHA256(data []byte, want string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("archive checksum mismatch")
	}
	return nil
}

func extractAndStage(archiveData []byte, assetName, goos, stageDir string) (stagedPath string, stagedSHA string, err error) {
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create staging dir: %w", err)
	}

	wantName := ExecutableBaseName(goos)
	var binary []byte
	switch {
	case strings.HasSuffix(assetName, ".tar.gz"):
		binary, err = extractTarGz(archiveData, wantName)
	default:
		return "", "", fmt.Errorf("unsupported archive type %q", assetName)
	}
	if err != nil {
		return "", "", err
	}
	if int64(len(binary)) > maxExpandedCandidate {
		return "", "", fmt.Errorf("expanded binary exceeds %d byte limit", maxExpandedCandidate)
	}

	sum := sha256.Sum256(binary)
	stagedSHA = hex.EncodeToString(sum[:])

	stagedPath, err = writeCandidateExclusive(stageDir, binary)
	if err != nil {
		return "", "", err
	}
	return stagedPath, stagedSHA, nil
}

func writeCandidateExclusive(stageDir string, binary []byte) (string, error) {
	path := candidatePath(stageDir)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return "", fmt.Errorf("stage candidate: %w", err)
	}
	if _, err := f.Write(binary); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write candidate: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close candidate: %w", err)
	}
	return path, nil
}

func extractTarGz(data []byte, wantName string) ([]byte, error) {
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open gzip archive: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var found []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}
		if err := validateArchiveHeader(hdr.Name, hdr.Typeflag, wantName); err != nil {
			return nil, err
		}
		if found != nil {
			return nil, fmt.Errorf("archive contains multiple executable files")
		}
		body, err := io.ReadAll(io.LimitReader(tr, maxExpandedCandidate+1))
		if err != nil {
			return nil, fmt.Errorf("read tar member: %w", err)
		}
		if int64(len(body)) > maxExpandedCandidate {
			return nil, fmt.Errorf("archive member exceeds size limit")
		}
		found = body
	}
	if found == nil {
		return nil, fmt.Errorf("archive did not contain %q", wantName)
	}
	return found, nil
}

func cleanArchiveName(name string) string {
	name = strings.TrimPrefix(name, "./")
	return filepath.ToSlash(filepath.Clean(name))
}

func validateArchivePathRaw(name string) error {
	if name == "" {
		return nil
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("archive absolute path rejected: %q", name)
	}
	if strings.HasPrefix(name, "\\\\") {
		return fmt.Errorf("archive absolute path rejected: %q", name)
	}
	if len(name) >= 2 && name[1] == ':' {
		return fmt.Errorf("archive absolute path rejected: %q", name)
	}
	if strings.Contains(name, "\\") {
		slash := strings.ReplaceAll(name, "\\", "/")
		if strings.HasPrefix(slash, "../") || strings.Contains(slash, "/../") || strings.HasPrefix(slash, "/") {
			return fmt.Errorf("archive path traversal rejected: %q", name)
		}
	}
	if strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return fmt.Errorf("archive path traversal rejected: %q", name)
	}
	return nil
}

func validateArchiveHeader(name string, typ byte, wantName string) error {
	if err := validateArchivePathRaw(name); err != nil {
		return err
	}
	clean := cleanArchiveName(name)
	if clean == "." || clean == "" {
		return fmt.Errorf("archive contains unexpected entry %q", name)
	}
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || filepath.IsAbs(clean) {
		return fmt.Errorf("archive path traversal rejected: %q", name)
	}
	if strings.Count(clean, "/") > 0 {
		return fmt.Errorf("archive contains nested path %q", name)
	}
	if clean != wantName {
		return fmt.Errorf("unexpected archive member %q", name)
	}
	switch typ {
	case tar.TypeReg, tar.TypeRegA:
		return nil
	default:
		return fmt.Errorf("archive selected entry %q is not a regular file", name)
	}
}
