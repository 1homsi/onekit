package onek

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	goosWindows        = "windows"
	DefaultReleaseBase = "https://github.com/1homsi/onekit/releases/download"
	releaseBaseEnv     = "ONEK_DOWNLOAD_BASE"
	maxReleaseDownload = 256 << 20
)

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$`)

// NormalizeVersion trims a leading "v" so "v0.25.5" and "0.25.5" compare equal.
func NormalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func validatePinnedVersion(version string) error {
	if version == "" {
		return nil
	}
	if !versionPattern.MatchString(NormalizeVersion(version)) {
		return fmt.Errorf("version must be a release such as \"0.25.5\", not %q", version)
	}
	return nil
}

// PinnedVersion returns the release a project pins with `version = "x.y.z"` in
// its onekit.toml, or "" when there is no config or no pin. It reads only that
// key, so it works for projects whose other settings a different release added.
func PinnedVersion(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, configFileName))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var pin struct {
		Version string `toml:"version"`
	}
	if _, err := toml.Decode(string(data), &pin); err != nil {
		return "", fmt.Errorf("%s: %w", configFileName, err)
	}
	if err := validatePinnedVersion(pin.Version); err != nil {
		return "", fmt.Errorf("%s: %w", configFileName, err)
	}
	return NormalizeVersion(pin.Version), nil
}

// ReleaseSource describes where release archives come from and where the
// verified binaries are cached.
type ReleaseSource struct {
	BaseURL  string
	CacheDir string
	GOOS     string
	GOARCH   string
	Client   *http.Client
}

func (s ReleaseSource) base() string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/")
	}
	if env := os.Getenv(releaseBaseEnv); env != "" {
		return strings.TrimRight(env, "/")
	}
	return DefaultReleaseBase
}

func (s ReleaseSource) cacheDir() (string, error) {
	if s.CacheDir != "" {
		return s.CacheDir, nil
	}
	if env := os.Getenv(cacheDirEnv); env != "" {
		return env, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "onekit"), nil
}

func releaseAssetName(version, goos, goarch string) (string, error) {
	var osName string
	switch goos {
	case "linux":
		osName = "Linux"
	case "darwin":
		osName = "Darwin"
	case goosWindows:
		osName = "Windows"
	default:
		return "", fmt.Errorf("no onekit release for %s", goos)
	}
	var arch string
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("no onekit release for %s/%s", goos, goarch)
	}
	ext := "tar.gz"
	if goos == goosWindows {
		ext = "zip"
	}
	return fmt.Sprintf("onekit_%s_%s_%s.%s", version, osName, arch, ext), nil
}

func binaryName(goos string) string {
	if goos == goosWindows {
		return "onek.exe"
	}
	return "onek"
}

func (s ReleaseSource) download(ctx context.Context, url string) ([]byte, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseDownload+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if len(data) > maxReleaseDownload {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, maxReleaseDownload)
	}
	return data, nil
}

func expectedChecksum(checksums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", asset)
}

// Fetch returns the path of the verified onek binary for version, downloading
// and caching it when needed. The archive's SHA-256 must match the release's
// checksums.txt.
func (s ReleaseSource) Fetch(ctx context.Context, version string) (string, error) {
	version = NormalizeVersion(version)
	if err := validatePinnedVersion(version); err != nil {
		return "", err
	}
	goos, goarch := s.GOOS, s.GOARCH
	asset, err := releaseAssetName(version, goos, goarch)
	if err != nil {
		return "", err
	}
	cache, err := s.cacheDir()
	if err != nil {
		return "", fmt.Errorf("find a cache directory: %w", err)
	}
	dest := filepath.Join(cache, "releases", version, binaryName(goos))
	if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() {
		return dest, nil
	}
	tag := s.base() + "/v" + version + "/"
	checksums, err := s.download(ctx, tag+"checksums.txt")
	if err != nil {
		return "", err
	}
	want, err := expectedChecksum(checksums, asset)
	if err != nil {
		return "", err
	}
	archive, err := s.download(ctx, tag+asset)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return "", fmt.Errorf("checksum mismatch for %s: release lists %s, downloaded %s", asset, want, got)
	}
	binary, err := extractBinary(archive, asset, binaryName(goos))
	if err != nil {
		return "", fmt.Errorf("unpack %s: %w", asset, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".onek-download-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0o700); err != nil { //nolint:gosec // the cached binary must be executable
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return dest, nil
}

func extractBinary(archive []byte, asset, name string) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if filepath.Base(file.Name) != name || file.FileInfo().IsDir() {
				continue
			}
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, maxReleaseDownload))
		}
		return nil, fmt.Errorf("no %s in the archive", name)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == name {
			return io.ReadAll(io.LimitReader(tr, maxReleaseDownload))
		}
	}
}
