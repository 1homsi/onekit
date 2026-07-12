package gengo

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const validateExtendedFixture = `
package main

message CreateProductRequest {
  name: string @required
  price: float64 @gte(0)
  lead_time_days: int32 @gt(0) @lte(1000)
  redirect_uri: string @required @uri
  launch_date: string @pattern("^[0-9]{4}-[0-9]{2}-[0-9]{2}$")
  tags: string[] @min_items(1) @max_items(5)
}

message ScheduleEventRequest {
  // Same field name as CreateProductRequest.launch_date but a DIFFERENT
  // pattern - proves pattern vars are scoped per-message, not just per-field.
  launch_date: string @pattern("^[0-9]{2}/[0-9]{2}/[0-9]{4}$")
}
`

const validateExtendedHarness = `
package main

import (
	"fmt"
	"os"
	"strings"
)

func fail(msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(1)
}

func expectValid(name string, req *CreateProductRequest) {
	if err := req.Validate(); err != nil {
		fail("%s: expected valid, got error: %v", name, err)
	}
}

func expectInvalid(name string, req *CreateProductRequest, substr string) {
	err := req.Validate()
	if err == nil {
		fail("%s: expected validation error, got nil", name)
	}
	if !strings.Contains(err.Error(), substr) {
		fail("%s: expected error containing %q, got: %v", name, substr, err)
	}
}

func main() {
	valid := &CreateProductRequest{
		Name:          "Widget",
		Price:         0,
		LeadTimeDays:  1000,
		RedirectUri:   "https://example.com/cb",
		LaunchDate:    "2026-01-15",
		Tags:          []string{"a"},
	}
	expectValid("valid", valid)

	negPrice := *valid
	negPrice.Price = -0.01
	expectInvalid("negative price", &negPrice, "price must be greater than or equal to 0")

	zeroLeadTime := *valid
	zeroLeadTime.LeadTimeDays = 0
	expectInvalid("zero lead time", &zeroLeadTime, "lead_time_days must be greater than 0")

	overLeadTime := *valid
	overLeadTime.LeadTimeDays = 1001
	expectInvalid("over lead time", &overLeadTime, "lead_time_days must be less than or equal to 1000")

	badURI := *valid
	badURI.RedirectUri = "not-a-uri"
	expectInvalid("bad uri", &badURI, "redirect_uri must be a valid uri")

	badDate := *valid
	badDate.LaunchDate = "01-15-2026"
	expectInvalid("bad date pattern", &badDate, "launch_date has an invalid format")

	noTags := *valid
	noTags.Tags = nil
	expectInvalid("no tags", &noTags, "tags must have at least 1 items")

	tooManyTags := *valid
	tooManyTags.Tags = []string{"a", "b", "c", "d", "e", "f"}
	expectInvalid("too many tags", &tooManyTags, "tags must have at most 5 items")

	// Cross-message pattern var collision check: ScheduleEventRequest's
	// launch_date uses a DIFFERENT pattern than CreateProductRequest's.
	validEvent := &ScheduleEventRequest{LaunchDate: "01/15/2026"}
	if err := validEvent.Validate(); err != nil {
		fail("valid event: expected valid, got error: %v", err)
	}
	invalidEvent := &ScheduleEventRequest{LaunchDate: "2026-01-15"}
	if err := invalidEvent.Validate(); err == nil {
		fail("invalid event: expected validation error, got nil")
	} else if !strings.Contains(err.Error(), "launch_date has an invalid format") {
		fail("invalid event: unexpected error: %v", err)
	}

	fmt.Println("OK")
}
`

func TestExtendedValidationDecorators(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	ast, err := onklang.Parse(validateExtendedFixture)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]

	typesSrc, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("GenerateTypes error: %v\n%s", err, typesSrc)
	}
	validateSrc, err := GenerateValidation(file)
	if err != nil {
		t.Fatalf("GenerateValidation error: %v\n%s", err, validateSrc)
	}
	if len(validateSrc) == 0 {
		t.Fatalf("expected non-empty generated validation code")
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_validate_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "types.go"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "validate.go"), string(validateSrc))
	writeFile(t, filepath.Join(dir, "main.go"), validateExtendedHarness)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}
