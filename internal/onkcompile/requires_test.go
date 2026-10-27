package onkcompile

import (
	"strings"
	"testing"
)

func compileRequires(t *testing.T, decorators string) error {
	t.Helper()
	_, err := Compile([]Source{{Path: "api.tsp", AST: parseOrFatal(t, "model R {}\ninterface API {\n  "+decorators+" a(...R): R;\n}\n")}})
	return err
}

func TestCompileAcceptsRequiresOnHTTPAndStreamMethods(t *testing.T) {
	for _, decorators := range []string{
		`@get @route("/a") @Onkt.requires("items:read")`,
		`@post @route("/a") @Onkt.requires("items:read", "items:write", "admin/all", "a.b-c_d")`,
		`@get @route("/a") @Onkt.stream @Onkt.requires("events:read")`,
	} {
		if err := compileRequires(t, decorators); err != nil {
			t.Fatalf("%s: %v", decorators, err)
		}
	}
}

func TestCompileRejectsInvalidRequires(t *testing.T) {
	for decorators, want := range map[string]string{
		`@get @route("/a") @Onkt.requires`:                            "at least one scope",
		`@get @route("/a") @Onkt.requires("items read")`:              "invalid @requires scope",
		`@get @route("/a") @Onkt.requires("")`:                        "invalid @requires scope",
		`@get @route("/a") @Onkt.requires("a", "a")`:                  `duplicate @requires scope "a"`,
		`@get @route("/a") @Onkt.requires("a") @Onkt.requires("b")`:   "single @requires",
		`@Onkt.ws @route("/a") @Onkt.requires("a")`:                   "not supported on @ws methods yet",
		`@get @route("/a") @Onkt.requires("items:read", "wild*card")`: "invalid @requires scope",
	} {
		err := compileRequires(t, decorators)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want error containing %q, got %v", decorators, want, err)
		}
	}
}
