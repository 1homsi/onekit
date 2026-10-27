package onek

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var typespecBlock = regexp.MustCompile("(?s)```typespec\n(.*?)```")

// TestReadmeTypeSpecExamplesCompile compiles every TypeSpec example in the
// README and docs. Examples may leave out types the prose already
// introduced, so an unknown name is the only error allowed.
func TestReadmeTypeSpecExamplesCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("runs tsp once per example")
	}
	repoNodeModules(t)
	var blocks []string
	for _, doc := range []string{"README.md", filepath.Join("docs", "gin-migration.md")} {
		data, err := os.ReadFile(filepath.Join("..", "..", doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range typespecBlock.FindAllStringSubmatch(string(data), -1) {
			blocks = append(blocks, match[1])
		}
	}
	if len(blocks) < 15 {
		t.Fatalf("found %d TypeSpec examples", len(blocks))
	}
	clean := 0
	for i, block := range blocks {
		dir := t.TempDir()
		linkTypeSpec(t, dir)
		if !strings.Contains(block, "import \"@onkt/typespec\"") {
			block = "import \"@typespec/http\";\nimport \"@onkt/typespec\";\nusing TypeSpec.Http;\nnamespace readme;\n" + snippetBody(block)
		}
		writeTestFile(t, filepath.Join(dir, "main.tsp"), block)
		_, err := runTypeSpec(dir, filepath.Join(dir, "main.tsp"))
		if err == nil {
			clean++
			continue
		}
		var tspErr *TypeSpecError
		if !errors.As(err, &tspErr) {
			t.Errorf("example %d: %v", i, err)
			continue
		}
		for _, d := range tspErr.Diagnostics() {
			if d.Code != "invalid-ref" && d.Code != "unknown-identifier" {
				t.Errorf("example %d: %s %s\n%s", i, d.Code, d.Message, block)
			}
		}
	}
	if clean < 4 {
		t.Fatalf("only %d examples compiled without unknown names", clean)
	}
}

// snippetBody wraps operations written on their own in an interface.
func snippetBody(block string) string {
	if declarationLine.MatchString(block) {
		return block
	}
	return "interface Snippet {\n" + block + "}\n"
}

var declarationLine = regexp.MustCompile(`(?m)^\s*(model|union|interface|enum|alias|namespace)\b`)
