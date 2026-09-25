package main

import (
	"bytes"
	"testing"

	"github.com/1homsi/onekit/internal/onek"
)

func TestGitHubAnnotations(t *testing.T) {
	var out bytes.Buffer
	writeGitHubAnnotations(&out, []onek.Diagnostic{
		{Path: "api/models.onk", Line: 3, Column: 7, Code: "compile_error", Message: "unresolved type \"X\"\nsecond line"},
		{Message: "config problem"},
	})
	want := "::error file=api/models.onk,line=3,col=7,title=onek compile_error::unresolved type \"X\"%0Asecond line\n::error ::config problem\n"
	if out.String() != want {
		t.Fatalf("got %q\nwant %q", out.String(), want)
	}
}
