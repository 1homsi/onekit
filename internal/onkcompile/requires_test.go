package onkcompile

import (
	"strings"
	"testing"
)

func compileRequires(t *testing.T, rpc string) error {
	t.Helper()
	_, err := Compile([]Source{{Path: "api.onk", AST: parseOrFatal(t, "message R {}\nservice API {\n  "+rpc+"\n}\n")}})
	return err
}

func TestCompileAcceptsRequiresOnHTTPAndStreamMethods(t *testing.T) {
	for _, rpc := range []string{
		`a(R) -> R @get("/a") @requires("items:read")`,
		`a(R) -> R @post("/a") @requires("items:read", "items:write", "admin/all", "a.b-c_d")`,
		`a(R) -> R @get("/a") @stream @requires("events:read")`,
	} {
		if err := compileRequires(t, rpc); err != nil {
			t.Fatalf("%s: %v", rpc, err)
		}
	}
}

func TestCompileRejectsInvalidRequires(t *testing.T) {
	for rpc, want := range map[string]string{
		`a(R) -> R @get("/a") @requires`:                            "at least one scope",
		`a(R) -> R @get("/a") @requires("items read")`:              "invalid @requires scope",
		`a(R) -> R @get("/a") @requires("")`:                        "invalid @requires scope",
		`a(R) -> R @get("/a") @requires("a", "a")`:                  `duplicate @requires scope "a"`,
		`a(R) -> R @get("/a") @requires("a") @requires("b")`:        "single @requires",
		`a(R) -> R @ws("/a") @requires("a")`:                        "not supported on @ws methods yet",
		`a(R) -> R @get("/a") @requires("items:read", "wild*card")`: "invalid @requires scope",
	} {
		err := compileRequires(t, rpc)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want error containing %q, got %v", rpc, want, err)
		}
	}
}
