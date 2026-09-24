package onek

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyGeneratedReportsDriftWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), onekitToml)
	writeTestFile(t, filepath.Join(dir, "models.onk"), modelsOnk)
	writeTestFile(t, filepath.Join(dir, "service.onk"), serviceOnk)

	var drift *DriftError
	if err := VerifyGenerated(dir); !errors.As(err, &drift) || !strings.Contains(err.Error(), "api/types.gen.go (missing)") {
		t.Fatalf("want missing outputs, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api")); !os.IsNotExist(err) {
		t.Fatalf("verify wrote output: %v", err)
	}
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGenerated(dir); err != nil {
		t.Fatalf("fresh build reported drift: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "api", "types.gen.go"), "package api\n")
	if err := VerifyGenerated(dir); !errors.As(err, &drift) || len(drift.Files) != 1 || drift.Files[0] != "api/types.gen.go" {
		t.Fatalf("want edited file reported, got %v", err)
	}
}
