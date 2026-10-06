package onek

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tsFieldNamesSchema = `package fe

message Req { folder_id: string }
message Res { is_default: bool }

service Svc {
  base_path: "/v1"
  getThing(Req) -> Res @get("/things/{folder_id}")
}
`

func buildTSFieldNames(t *testing.T, target, fieldNames string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	option := ""
	if fieldNames != "" {
		option = "field_names = \"" + fieldNames + "\"\n"
	}
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), "module = \"example.com/fe\"\n\n[generate."+target+"]\nout = \"gen\"\n"+option)
	writeTestFile(t, filepath.Join(dir, "svc.onk"), tsFieldNamesSchema)
	err := Build(dir)
	return dir, err
}

func TestBuildTSFieldNamesOption(t *testing.T) {
	for _, target := range []string{"ts-client", "ts-server"} {
		for mode, want := range map[string]string{"wire": "is_default", "camel": "isDefault", "": "isDefault"} {
			dir, err := buildTSFieldNames(t, target, mode)
			if err != nil {
				t.Fatalf("%s %q: %v", target, mode, err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "gen", "types.ts"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "export interface Res {\n"+want+"?: boolean;") && !strings.Contains(string(data), want+"?: boolean") {
				t.Errorf("%s %q: expected property %q in types.ts:\n%s", target, mode, want, data)
			}
		}
	}
}

func TestBuildTSFieldNamesRejectsUnknownValue(t *testing.T) {
	_, err := buildTSFieldNames(t, "ts-client", "snake")
	if err == nil || !strings.Contains(err.Error(), `field_names must be "camel" or "wire"`) {
		t.Fatalf("error = %v", err)
	}
}
