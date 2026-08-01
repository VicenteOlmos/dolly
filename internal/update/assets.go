package update

import (
	"fmt"
	"runtime"
)

// Release asset names shared with .github/workflows/release.yml, install.sh,
// install.ps1, and scripts/build-release-assets.sh.
var releaseAssetNames = []string{
	"dolly_linux_x86_64.tar.gz",
	"dolly_linux_arm64.tar.gz",
	"dolly_darwin_x86_64.tar.gz",
	"dolly_darwin_arm64.tar.gz",
	"dolly_windows_x86_64.zip",
	"dolly_windows_arm64.zip",
	"checksums.txt",
}

var assetByPlatform = map[string]string{
	"linux/amd64":   "dolly_linux_x86_64.tar.gz",
	"linux/arm64":   "dolly_linux_arm64.tar.gz",
	"darwin/amd64":  "dolly_darwin_x86_64.tar.gz",
	"darwin/arm64":  "dolly_darwin_arm64.tar.gz",
	"windows/amd64": "dolly_windows_x86_64.zip",
	"windows/arm64": "dolly_windows_arm64.zip",
}

// ReleaseAssetNames returns the exact release asset filenames in stable order.
func ReleaseAssetNames() []string {
	out := make([]string, len(releaseAssetNames))
	copy(out, releaseAssetNames)
	return out
}

// AssetName returns the release archive name for goos/goarch.
func AssetName(goos, goarch string) (string, error) {
	name, ok := assetByPlatform[goos+"/"+goarch]
	if !ok {
		return "", fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}
	return name, nil
}

// CurrentAsset returns the release archive name for the running binary.
func CurrentAsset() (string, error) {
	return AssetName(runtime.GOOS, runtime.GOARCH)
}

// ExecutableBaseName returns the binary name inside release archives.
func ExecutableBaseName(goos string) string {
	if goos == "windows" {
		return "dolly.exe"
	}
	return "dolly"
}
