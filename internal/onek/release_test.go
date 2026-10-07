package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedVersionReadsOnlyTheVersionKey(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := PinnedVersion(dir); err != nil || got != "" {
		t.Fatalf("no config: %q %v", got, err)
	}
	write("version = \"v0.26.0\"\nsomething_from_the_future = true\n")
	if got, err := PinnedVersion(dir); err != nil || got != "0.26.0" {
		t.Fatalf("got %q %v", got, err)
	}
	write("version = \"latest\"\n")
	if _, err := PinnedVersion(dir); err == nil || !strings.Contains(err.Error(), "release such as") {
		t.Fatalf("a non-release pin must be rejected, got %v", err)
	}
}

func TestReleaseAssetNamesMatchTheReleaseArchives(t *testing.T) {
	cases := map[[2]string]string{
		{"darwin", "arm64"}:  "onekit_1.2.3_Darwin_arm64.tar.gz",
		{"linux", "amd64"}:   "onekit_1.2.3_Linux_x86_64.tar.gz",
		{"windows", "amd64"}: "onekit_1.2.3_Windows_x86_64.zip",
	}
	for in, want := range cases {
		got, err := releaseAssetName("1.2.3", in[0], in[1])
		if err != nil || got != want {
			t.Errorf("%v: got %q %v want %q", in, got, err, want)
		}
	}
	if _, err := releaseAssetName("1.2.3", "plan9", "amd64"); err == nil {
		t.Error("an unsupported platform must be an error")
	}
}
