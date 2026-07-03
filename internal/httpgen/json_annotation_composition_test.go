package httpgen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectJSONEncodingCompositionConflictFailsGeneration(t *testing.T) {
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not found, skipping generator integration test")
	}

	baseDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	projectRoot := filepath.Join(baseDir, "..", "..")
	tempDir := t.TempDir()
	pluginPath := filepath.Join(tempDir, "protoc-gen-onekit-go-http")

	buildCmd := exec.Command("go", "build", "-o", pluginPath, "./cmd/protoc-gen-onekit-go-http")
	buildCmd.Dir = projectRoot
	if output, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		t.Fatalf("build go-http plugin: %v\n%s", buildErr, output)
	}

	proto := `syntax = "proto3";

package testdata.jsoncomposition;

option go_package = "example.com/jsoncomposition;jsoncomposition";

import "onekit/http/annotations.proto";

message Combo {
  optional string display_name = 1 [(onekit.http.nullable) = true];
  bytes token = 2 [(onekit.http.bytes_encoding) = BYTES_ENCODING_HEX];
}
`
	protoPath := filepath.Join(tempDir, "combo.proto")
	if writeErr := os.WriteFile(protoPath, []byte(proto), 0o644); writeErr != nil {
		t.Fatalf("write proto fixture: %v", writeErr)
	}

	cmd := exec.Command("protoc",
		"--plugin=protoc-gen-onekit-go-http="+pluginPath,
		"--onekit-go-http_out="+tempDir,
		"--onekit-go-http_opt=paths=source_relative",
		"--proto_path="+tempDir,
		"--proto_path="+filepath.Join(projectRoot, "proto"),
		"combo.proto",
	)
	cmd.Dir = tempDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr == nil {
		t.Fatal("expected generation to fail for multiple direct JSON encoding annotations")
	}

	got := stderr.String()
	for _, want := range []string{
		"json annotation validation failed",
		"multiple Go JSON encoding annotations",
		"nullable",
		"bytes_encoding",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr missing %q:\n%s", want, got)
		}
	}
}
