package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a stable MAJOR.MINOR.PATCH release version.
type Version struct {
	Major int
	Minor int
	Patch int
}

// ParseVersion parses v?MAJOR.MINOR.PATCH with optional +build metadata.
// Prerelease labels (-suffix), empty input, and dev builds are rejected.
func ParseVersion(raw string) (Version, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Version{}, fmt.Errorf("version is empty; build or install a release binary to use update")
	}
	if strings.EqualFold(s, "dev") {
		return Version{}, fmt.Errorf("development build cannot self-update; install a release binary from GitHub releases")
	}
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")

	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		return Version{}, fmt.Errorf("prerelease version %q is not supported", raw)
	}
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		if err := validateBuildMetadata(s[idx+1:]); err != nil {
			return Version{}, fmt.Errorf("invalid version %q", raw)
		}
		s = s[:idx]
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid version %q", raw)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return Version{}, fmt.Errorf("invalid version %q", raw)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return Version{}, fmt.Errorf("invalid version %q", raw)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil || patch < 0 {
		return Version{}, fmt.Errorf("invalid version %q", raw)
	}

	return Version{Major: major, Minor: minor, Patch: patch}, nil
}

// validateBuildMetadata enforces SemVer 2.0 build metadata: +identifier(.identifier)*.
func validateBuildMetadata(build string) error {
	if build == "" {
		return fmt.Errorf("empty build metadata")
	}
	for _, part := range strings.Split(build, ".") {
		if part == "" {
			return fmt.Errorf("empty build identifier")
		}
		for _, c := range part {
			if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-' {
				continue
			}
			return fmt.Errorf("invalid build identifier character %q", c)
		}
	}
	return nil
}

// Compare returns -1 if v < other, 0 if equal, 1 if v > other.
func (v Version) Compare(other Version) int {
	switch {
	case v.Major != other.Major:
		if v.Major < other.Major {
			return -1
		}
		return 1
	case v.Minor != other.Minor:
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	case v.Patch != other.Patch:
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	default:
		return 0
	}
}

// String returns the canonical vX.Y.Z form.
func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// IsNewer reports whether remote is strictly newer than local.
func IsNewer(local, remote Version) bool {
	return remote.Compare(local) > 0
}
