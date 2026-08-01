package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

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
