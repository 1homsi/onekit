package genopenapi

import (
	"strings"
	"testing"
)

func TestOpenAPIOmitsServersWhenNoneAreConfigured(t *testing.T) {
	doc := openAPIForSchema(t, "package app\nmessage R {}\nservice S { get(R) -> R @get(\"/r\") }\n")
	if strings.Contains(doc, "servers:") {
		t.Fatalf("empty servers list emitted:\n%s", doc)
	}
}
