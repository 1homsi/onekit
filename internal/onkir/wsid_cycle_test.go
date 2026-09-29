package onkir_test

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
	"github.com/1homsi/onekit/internal/onkir"
)

func findMessage(t *testing.T, pkg *onkir.Package, name string) *onkir.Message {
	t.Helper()
	for _, f := range pkg.Files {
		for _, m := range f.Messages {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("message %q not found in IR", name)
	return nil
}

func compile(t *testing.T, src string) *onkir.Package {
	t.Helper()
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "probe.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return pkg
}

// A oneof variant may legally be typed as the enclosing message, or as any
// message mutually recursive with it. onkcompile performs no acyclicity check
// on type references, so Node's IR can contain a oneof whose variant points
// back at Node itself.
//
// WSIDField recursed into variant messages with no cycle guard, so it
// re-entered with the same *Message forever and killed every generator with a
// fatal stack overflow (not a recoverable panic).
const selfRecursiveOneof = `package w
message Node {
  payload: oneof(discriminator: "type") {
    leaf: Leaf
    child: Node
  }
  id: string @ws_id
}
message Leaf { x: string }
service Runtime {
  base_path: "/v1"
  execute(Node) -> Node @ws("/n")
}
`

func TestWSIDFieldSelfRecursiveOneofTerminates(t *testing.T) {
	node := findMessage(t, compile(t, selfRecursiveOneof), "Node")

	// Must return rather than overflow the stack. The direct @ws_id on Node is
	// not reached first because the oneof field precedes it.
	field, ok := onkir.WSIDField(node)
	if !ok {
		t.Fatal("expected to resolve a @ws_id field")
	}
	if field.Name != "id" {
		t.Errorf("WSIDField = %q, want %q", field.Name, "id")
	}
}

// Mutual recursion: A's oneof holds a B variant, B's oneof holds an A variant.
const mutuallyRecursiveOneof = `package w
message A {
  payload: oneof(discriminator: "type") {
    a: string
    b: B
  }
}
message B {
  payload: oneof(discriminator: "type") {
    c: string
    a: A
  }
  id: string @ws_id
}
service Runtime {
  base_path: "/v1"
  execute(A) -> B @ws("/ab")
}
`

func TestWSIDFieldMutuallyRecursiveOneofTerminates(t *testing.T) {
	a := findMessage(t, compile(t, mutuallyRecursiveOneof), "A")

	field, ok := onkir.WSIDField(a)
	if !ok {
		t.Fatal("expected to resolve a @ws_id field through mutual recursion")
	}
	if field.Name != "id" {
		t.Errorf("WSIDField = %q, want %q", field.Name, "id")
	}
}

// A plain self-referential message with no oneof at all must be unaffected.
const plainSelfReference = `package w
message Node { child: Node?  id: string @ws_id }
service Runtime {
  base_path: "/v1"
  execute(Node) -> Node @ws("/n")
}
`

func TestWSIDFieldPlainSelfReferenceUnchanged(t *testing.T) {
	node := findMessage(t, compile(t, plainSelfReference), "Node")

	field, ok := onkir.WSIDField(node)
	if !ok {
		t.Fatal("expected to resolve a @ws_id field")
	}
	if field.Name != "id" {
		t.Errorf("WSIDField = %q, want %q", field.Name, "id")
	}
}
