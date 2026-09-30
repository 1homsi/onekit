package genshared

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

func compile(t *testing.T, src string) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "a.onk", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Files[0]
}

func field(m *onkir.Message, name string) *onkir.Field {
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func TestNeedsInt64StringEncoding(t *testing.T) {
	file := compile(t, `package app
message M {
  plain: int64
  numbered: int64 @encode("number")
  text: string
}
`)
	m := file.Messages[0]
	if !NeedsInt64StringEncoding(field(m, "plain")) {
		t.Fatal("plain int64 should default to string encoding")
	}
	if NeedsInt64StringEncoding(field(m, "numbered")) {
		t.Fatal("@encode(number) int64 must not be string-encoded")
	}
	if NeedsInt64StringEncoding(field(m, "text")) {
		t.Fatal("a string field is never int64-string-encoded")
	}
	if NeedsInt64NumberEncoding(field(m, "plain")) {
		t.Fatal("plain int64 must not be number-encoded")
	}
	if !NeedsInt64NumberEncoding(field(m, "numbered")) {
		t.Fatal("@encode(number) int64 must be number-encoded")
	}
	if NeedsInt64NumberEncoding(field(m, "text")) {
		t.Fatal("a string field is never int64-number-encoded")
	}
}

func TestNeedsEnumNumberEncoding(t *testing.T) {
	file := compile(t, `package app
enum Status { ON OFF }
message M {
  plain: Status
  numbered: Status @encode("number")
  many: Status[]
}
`)
	m := file.Messages[0]
	if NeedsEnumNumberEncoding(field(m, "plain")) {
		t.Fatal("plain enum defaults to name encoding")
	}
	if !NeedsEnumNumberEncoding(field(m, "numbered")) {
		t.Fatal("@encode(number) enum must be number-encoded")
	}
	if NeedsEnumNumberEncoding(field(m, "many")) {
		t.Fatal("a repeated enum is never number-encoded (@encode is rejected on repeated fields)")
	}
}

func TestFlattenPrefixAndEmptyBehavior(t *testing.T) {
	file := compile(t, `package app
message Inner { note: string }
message M {
  flat: Inner @flatten(prefix: "f_")
  plain: Inner
  omitted: Inner @empty("omit")
}
`)
	m := file.Messages[1]
	prefix, ok := FlattenPrefix(field(m, "flat"))
	if !ok || prefix != "f_" {
		t.Fatalf("FlattenPrefix = %q, %v", prefix, ok)
	}
	if _, ok := FlattenPrefix(field(m, "plain")); ok {
		t.Fatal("an unflagged field must not report a flatten prefix")
	}
	if behavior := EmptyBehavior(field(m, "omitted")); behavior != "omit" {
		t.Fatalf("EmptyBehavior = %q", behavior)
	}
	if behavior := EmptyBehavior(field(m, "plain")); behavior != "" {
		t.Fatalf("EmptyBehavior of an unflagged field = %q", behavior)
	}
}

func TestRootUnwrapField(t *testing.T) {
	file := compile(t, `package app
message Ids { v: int64[] @unwrap }
message Plain { a: string b: string }
`)
	if f := RootUnwrapField(file.Messages[0]); f == nil || f.Name != "v" {
		t.Fatalf("RootUnwrapField(Ids) = %v", f)
	}
	if f := RootUnwrapField(file.Messages[1]); f != nil {
		t.Fatalf("RootUnwrapField(Plain) = %v, want nil", f)
	}
}

func TestOneofDiscriminator(t *testing.T) {
	file := compile(t, `package app
message A { v: string }
message M {
  default_disc: oneof { a: A @tag("a") }
  custom_disc: oneof(discriminator: "kind") { a: A @tag("a") }
}
`)
	m := file.Messages[1]
	if got := OneofDiscriminator(field(m, "default_disc")); got != "type" {
		t.Fatalf("default discriminator = %q", got)
	}
	if got := OneofDiscriminator(field(m, "custom_disc")); got != "kind" {
		t.Fatalf("custom discriminator = %q", got)
	}
}

func TestFileMessagesDeep(t *testing.T) {
	file := compile(t, `package app
message Outer {
  message Inner { note: string }
  child: Inner
}
message Plain { a: string }
`)
	var names []string
	for _, m := range FileMessagesDeep(file) {
		names = append(names, m.Name)
	}
	want := []string{"Outer", "Inner", "Plain"}
	if len(names) != len(want) {
		t.Fatalf("names = %v", names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}
