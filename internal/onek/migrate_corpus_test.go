package onek

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const corpusAllTargets = `module = "example.com/corpus/api"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"

[generate.ts-client]
out = "./gen/ts"

[generate.ts-server]
out = "./gen/ts"

[generate.python-client]
out = "./gen/py"

[generate.dart-client]
out = "./gen/dart"

[generate.swift-client]
out = "./gen/swift"

[generate.rust-client]
out = "./gen/rust"

[generate.rust-server]
out = "./gen/rust"

[generate.openapi]
out = "./gen/openapi"
`

const corpusWebTargets = `module = "example.com/corpus/api"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"

[generate.ts-client]
out = "./gen/ts"

[generate.openapi]
out = "./gen/openapi"
`

// TestMigrateCorpus converts every schema in testdata/migrate-corpus (the
// .onk schemas the rest of the test suite builds) to TypeSpec and requires
// the TypeSpec project to generate exactly what the .onk project generates,
// for every target. $ONK_CORPUS points it at another directory of schemas.
func TestMigrateCorpus(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds every corpus schema twice")
	}
	corpus := os.Getenv("ONK_CORPUS")
	if corpus == "" {
		corpus = filepath.Join("testdata", "migrate-corpus")
	}
	repoNodeModules(t)
	files, err := filepath.Glob(filepath.Join(corpus, "*.onk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		t.Run(strings.TrimSuffix(filepath.Base(file), ".onk"), func(t *testing.T) {
			t.Parallel()
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			checkMigration(t, string(source))
		})
	}
}

func checkMigration(t *testing.T, source string) {
	t.Helper()
	base := t.TempDir()
	onkDir := filepath.Join(base, "onk")
	var config string
	for _, candidate := range []string{corpusAllTargets, corpusWebTargets} {
		writeCorpusProject(t, onkDir, candidate, source)
		if err := Build(onkDir); err == nil {
			config = candidate
			break
		}
		os.RemoveAll(onkDir)
	}
	if config == "" {
		t.Skip("the .onk schema does not build")
	}

	tspDir := filepath.Join(base, "tsp")
	writeCorpusProject(t, tspDir, config, source)
	if _, err := Migrate(tspDir, MigrateOptions{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	linkTypeSpec(t, tspDir)
	if err := Build(tspDir); err != nil {
		t.Fatalf("build the migrated project: %v\n--- schema ---\n%s\n--- typespec ---\n%s", err, source, readTSP(t, tspDir))
	}
	want := generatedFiles(t, filepath.Join(onkDir, "gen"))
	got := generatedFiles(t, filepath.Join(tspDir, "gen"))
	var diffs []string
	for path, content := range want {
		if !bytes.Equal(got[path], content) {
			diffs = append(diffs, path)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			diffs = append(diffs, path+" (extra)")
		}
	}
	sort.Strings(diffs)
	if len(diffs) > 0 {
		t.Errorf("generated files differ: %s\n--- schema ---\n%s\n--- typespec ---\n%s", strings.Join(diffs, ", "), source, readTSP(t, tspDir))
		if dump := os.Getenv("ONK_CORPUS_DUMP"); dump != "" {
			name := filepath.Base(t.Name())
			for _, path := range diffs {
				path = strings.TrimSuffix(path, " (extra)")
				_ = os.MkdirAll(filepath.Join(dump, name, filepath.Dir(path)), 0o755)
				_ = os.WriteFile(filepath.Join(dump, name, path+".want"), want[path], 0o644)
				_ = os.WriteFile(filepath.Join(dump, name, path+".got"), got[path], 0o644)
			}
		}
	}
}

func writeCorpusProject(t *testing.T, dir, config, source string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.onk"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTSP(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	paths, _ := filepath.Glob(filepath.Join(dir, "*.tsp"))
	for _, path := range paths {
		data, _ := os.ReadFile(path)
		b.WriteString("// " + filepath.Base(path) + "\n")
		b.Write(data)
	}
	return b.String()
}

func generatedFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files
}
