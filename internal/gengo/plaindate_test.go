package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

// Booking takes the fast JSON path; Trip, with a flattened field, takes the
// encoding/json path with an aux struct. Both must send every item of a list
// or map of plainDate as YYYY-MM-DD, as they send a single one.
const plainDateListSchema = `namespace app;

model Booking {
  day: plainDate;
  days: plainDate[];
  byCity: Record<plainDate>;
}

model Where {
  city: string;
}

model Trip {
  day: plainDate;
  days: plainDate[];
  byCity: Record<plainDate>;
  @Onkt.flatten
  where: Where;
}
`

func TestGoPlainDateListsAndMapsSendEachItemAsADate(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	ast, err := onktest.Parse(plainDateListSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.CompileWithOptions([]onkcompile.Source{{Path: "app.tsp", AST: ast}}, onkcompile.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	types, err := GenerateTypesWithResolver(pkg.Files[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/dates\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "app", "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"encoding/json"
	"fmt"
	"time"

	app "example.com/dates/app"
)

func check(name string, v any, want string) {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	if string(out) != want {
		panic(fmt.Sprintf("%s sent as %s, want %s", name, out, want))
	}
	if err := json.Unmarshal([]byte(want), v); err != nil {
		panic(fmt.Sprintf("%s: decode %s: %v", name, want, err))
	}
	again, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	if string(again) != want {
		panic(fmt.Sprintf("%s decoded from %s sent back as %s", name, want, again))
	}
}

func main() {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	check("Booking", &app.Booking{Day: day, Days: []time.Time{day}, ByCity: map[string]time.Time{"x": day}},
		`+"`"+`{"day":"2026-11-03","days":["2026-11-03"],"byCity":{"x":"2026-11-03"}}`+"`"+`)
	check("Trip", &app.Trip{Day: day, Days: []time.Time{day}, ByCity: map[string]time.Time{"x": day}, Where: &app.Where{City: "Beirut"}},
		`+"`"+`{"byCity":{"x":"2026-11-03"},"city":"Beirut","day":"2026-11-03","days":["2026-11-03"]}`+"`"+`)
	var bad app.Booking
	if err := json.Unmarshal([]byte(`+"`"+`{"day":"2026-11-03","days":["2026-11-03T00:00:00Z"],"byCity":{}}`+"`"+`), &bad); err == nil {
		panic("a timestamp in a list of dates was accepted")
	}
	fmt.Println("OK")
}
`)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("go run: %v\n%s", err, out)
	}
}
