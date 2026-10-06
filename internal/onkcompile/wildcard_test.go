package onkcompile

import (
	"strings"
	"testing"
)

func TestWildcardPathValidation(t *testing.T) {
	cases := []struct {
		name    string
		service string
		want    string
	}{
		{"last segment accepted", `service S { read(Req) -> Res @get("/files/{path...}") }`, ""},
		{"middle segment rejected", `service S { read(Req) -> Res @get("/files/{path...}/x") }`, "must be the last segment"},
		{"partial segment rejected", `service S { read(Req) -> Res @get("/files/a{path...}") }`, "whole segment"},
		{"non-string field rejected", `service S { read(Req) -> Res @get("/files/{n...}") }`, "requires a string request field"},
		{"websocket rejected", `service S { open(Req) -> Res @ws("/files/{path...}") }`, "wildcard"},
		{"empty route without base path rejected", `service S { read(Req) -> Res @get("") }`, "base_path"},
		{"empty route with base path accepted", `service S { base_path: "/files"
  read(Req) -> Res @get("") }`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileRules(t, "package api\nmessage Req { path: string\n n: int32 }\nmessage Res { ok: bool }\n"+tc.service+"\n")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
