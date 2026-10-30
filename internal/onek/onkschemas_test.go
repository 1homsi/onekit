package onek

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectWithOnkSchemasIsRejected(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.onk"), "package app\nmessage M {}\n")
	err := Check(dir)
	if err == nil || !strings.Contains(err.Error(), "api.onk") || !strings.Contains(err.Error(), "no longer supported") || !strings.Contains(err.Error(), "0.39.x") {
		t.Fatalf("err = %v, want .onk named as unsupported with the 0.39.x hint", err)
	}
}

func TestPackageJSONPinsTheMatchingEmitterAndCLI(t *testing.T) {
	previous := toolVersion
	t.Cleanup(func() { SetToolVersion(previous) })
	for version, want := range map[string]string{"v0.37.0": `"@onkt/typespec": "^0.37.0"`, "dev": `"@onkt/typespec": "latest"`, "(devel)": `"@onkt/typespec": "latest"`} {
		cli := strings.Replace(want, "@onkt/typespec", "@onkt/cli", 1)
		SetToolVersion(version)
		if got := typespecPackageJSON(); !strings.Contains(got, want) || !strings.Contains(got, cli) {
			t.Errorf("%s: package.json = %s, want %s and %s", version, got, want, cli)
		}
	}
}
