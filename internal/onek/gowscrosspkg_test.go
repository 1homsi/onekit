package onek

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const wsCrossPackageCommonOnk = `
package common

message Ping {
  room: string
  text: string
}

message Pong {
  room: string
  text: string
}
`

const wsCrossPackageHubOnk = `
package hub

service Chat {
  chat(Ping) -> Pong @ws("/chat")
}
`

func TestGoWSWithCrossPackageFramesCompiles(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), `
module = "example.com/wsx/gen/go"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"
`)
	writeTestFile(t, filepath.Join(dir, "common", "frames.onk"), wsCrossPackageCommonOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "service.onk"), wsCrossPackageHubOnk)
	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}
	genRoot := filepath.Join(dir, "gen", "go")
	writeTestFile(t, filepath.Join(genRoot, "go.mod"), "module example.com/wsx/gen/go\n\ngo 1.26\n\nrequire github.com/coder/websocket v1.8.15\n")
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = genRoot
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	vet := exec.Command("go", "vet", "./...")
	vet.Dir = genRoot
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("generated cross-package WebSocket tree failed: %v\n%s", err, out)
	}
}
