package gengo

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const nullableBenchSchema = `package app

message App {
  id: int64 @encode("number")
  name: string
  slug: string
  folder_id: int64? @nullable @encode("number")
  owner: string
}
`

const nullableBenchFile = `package app

import (
	"encoding/json"
	"testing"
)

type legacyApp App

func (m *legacyApp) MarshalJSON() ([]byte, error) {
	type alias App
	aux := struct{ *alias }{alias: (*alias)(m)}
	base, err := json.Marshal(aux)
	if err != nil {
		return nil, err
	}
	if !(m.FolderIdNull && m.FolderId == nil) {
		return base, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	merged["folder_id"] = json.RawMessage("null")
	return json.Marshal(merged)
}

func (m *legacyApp) UnmarshalJSON(data []byte) error {
	type alias App
	aux := struct{ *alias }{alias: (*alias)(m)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["folder_id"]; ok && string(v) == "null" {
		m.FolderIdNull = true
	} else {
		m.FolderIdNull = false
	}
	return nil
}

func sample(state string) *App {
	app := &App{Id: 7, Name: "Billing", Slug: "billing", Owner: "ops"}
	switch state {
	case "set":
		id := int64(42)
		app.FolderId = &id
	case "null":
		app.FolderIdNull = true
	}
	return app
}

func TestNullableOutputMatchesTheTwoPassVersion(t *testing.T) {
	for _, state := range []string{"set", "null", "absent"} {
		app := sample(state)
		got, err := json.Marshal(app)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal((*legacyApp)(app))
		if err != nil {
			t.Fatal(err)
		}
		var a, b map[string]any
		if err := json.Unmarshal(got, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want, &b); err != nil {
			t.Fatal(err)
		}
		if len(a) != len(b) {
			t.Fatalf("%s: %s vs %s", state, got, want)
		}
		for key, value := range b {
			if a[key] != value {
				t.Fatalf("%s: %s differs: %s vs %s", state, key, got, want)
			}
		}
		var back, backLegacy App
		if err := json.Unmarshal(got, &back); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(got, (*legacyApp)(&backLegacy)); err != nil {
			t.Fatal(err)
		}
		if back.FolderIdNull != backLegacy.FolderIdNull || (back.FolderId == nil) != (backLegacy.FolderId == nil) {
			t.Fatalf("%s: decode differs: %+v vs %+v", state, back, backLegacy)
		}
	}
}

func benchMarshal(b *testing.B, state string, legacy bool) {
	app := sample(state)
	var value any = app
	if legacy {
		value = (*legacyApp)(app)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(value); err != nil {
			b.Fatal(err)
		}
	}
}

func benchUnmarshal(b *testing.B, state string, legacy bool) {
	data, err := json.Marshal(sample(state))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var target any = new(App)
		if legacy {
			target = new(legacyApp)
		}
		if err := json.Unmarshal(data, target); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalNullSingleShadowPass(b *testing.B)   { benchMarshal(b, "null", false) }
func BenchmarkMarshalNullLegacyThreePassMerge(b *testing.B)    { benchMarshal(b, "null", true) }
func BenchmarkMarshalAbsentSingleShadowPass(b *testing.B) { benchMarshal(b, "absent", false) }
func BenchmarkMarshalAbsentLegacyAliasPass(b *testing.B)    { benchMarshal(b, "absent", true) }
func BenchmarkMarshalSetSingleShadowPass(b *testing.B)    { benchMarshal(b, "set", false) }
func BenchmarkMarshalSetLegacyAliasPass(b *testing.B)       { benchMarshal(b, "set", true) }
func BenchmarkUnmarshalNullShadow(b *testing.B)           { benchUnmarshal(b, "null", false) }
func BenchmarkUnmarshalNullLegacyTwoDecodes(b *testing.B) { benchUnmarshal(b, "null", true) }
func BenchmarkUnmarshalSetShadow(b *testing.B)            { benchUnmarshal(b, "set", false) }
func BenchmarkUnmarshalSetLegacyTwoDecodes(b *testing.B)  { benchUnmarshal(b, "set", true) }
`

func TestNullableMarshalersBenchmarkAgainstTheTwoPassVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("benchmarks run in the full suite only")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	file := compileFixtureSource(t, nullableBenchSchema)
	types, err := GenerateTypesWithResolver(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nb\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "bench_test.go"), nullableBenchFile)
	cmd := exec.Command("go", "test", "-run", "TestNullableOutputMatchesTheTwoPassVersion", "-bench", ".", "-benchmem", "-benchtime", "20000x", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("\n%s", out)
}
