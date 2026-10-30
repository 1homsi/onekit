package onek

import (
	"strings"
	"testing"
)

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
