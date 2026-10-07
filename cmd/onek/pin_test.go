package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeReleaseArchive(t *testing.T) []byte {
	t.Helper()
	script := "#!/bin/sh\necho \"pinned binary: $*\"\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "onek", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func releaseAssetFor(version string) string {
	osName := map[string]string{"linux": "Linux", "darwin": "Darwin"}[runtime.GOOS]
	arch := map[string]string{"amd64": "x86_64", "arm64": "arm64"}[runtime.GOARCH]
	return fmt.Sprintf("onekit_%s_%s_%s.tar.gz", version, osName, arch)
}

func TestPinnedVersionDownloadsVerifiesAndRunsTheRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake release is a shell script")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	archive := fakeReleaseArchive(t)
	sum := sha256.Sum256(archive)
	asset := releaseAssetFor("9.9.9")
	checksum := hex.EncodeToString(sum[:])
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/v9.9.9/" + asset:
			_, _ = w.Write(archive)
		case "/v9.9.9/checksums.txt":
			_, _ = fmt.Fprintf(w, "%s  %s\n", checksum, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bin := filepath.Join(t.TempDir(), "onek")
	build := exec.Command("go", "build", "-ldflags", "-X main.version=0.0.1", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "onekit.toml"), []byte("version = \"9.9.9\"\nmodule = \"example.com/x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(extraEnv ...string) (string, error) {
		cmd := exec.Command(bin, "check", "--dir", project)
		cmd.Env = append(os.Environ(), "ONEK_DOWNLOAD_BASE="+server.URL, "ONEK_CACHE_DIR="+filepath.Join(project, "cache"))
		cmd.Env = append(cmd.Env, extraEnv...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := run()
	if err != nil || !strings.Contains(out, "pinned binary: check --dir "+project) {
		t.Fatalf("expected the pinned release to run, got %v\n%s", err, out)
	}
	firstHits := hits
	if out, err = run(); err != nil || !strings.Contains(out, "pinned binary") {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	if hits != firstHits {
		t.Fatalf("a cached release must not be downloaded again (%d -> %d requests)", firstHits, hits)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			_, _ = fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), asset)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer bad.Close()
	cmd := exec.Command(bin, "check", "--dir", project)
	cmd.Env = append(os.Environ(), "ONEK_DOWNLOAD_BASE="+bad.URL, "ONEK_CACHE_DIR="+t.TempDir())
	out2, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out2), "checksum mismatch") {
		t.Fatalf("a tampered archive must be refused, got %v\n%s", err, out2)
	}
}

func TestPinDirectoryFindsTheProject(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		want    string
	}{
		{"build", nil, "."},
		{"build", []string{"proj"}, "proj"},
		{"check", []string{"--format", "json", "proj"}, "proj"},
		{"build", []string{"--dir", "a"}, "a"},
		{"watch", []string{"--dir=b", "--interval", "1s"}, "b"},
		{"mock", []string{"--addr", ":1"}, "."},
	}
	for _, c := range cases {
		if got := pinDirectory(c.command, c.args); got != c.want {
			t.Errorf("%s %v: got %q want %q", c.command, c.args, got, c.want)
		}
	}
}
