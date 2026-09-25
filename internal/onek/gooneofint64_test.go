package onek

import "testing"

func TestGoOneofInt64VariantsUseStrings(t *testing.T) {
	buildGoSchema(t, `
package check

message Box {
  value: oneof {
    big: int64 @tag("big")
    top: uint64 @tag("top")
  }
}
`, `package api

import (
	"encoding/json"
	"testing"
)

func TestOneofInt64(t *testing.T) {
	data, err := json.Marshal(&Box{Value: &BoxValueBig{Big: -9007199254740993}})
	if err != nil || string(data) != `+"`"+`{"value":{"type":"big","big":"-9007199254740993"}}`+"`"+` {
		t.Fatalf("marshal = %s, %v", data, err)
	}
	var got Box
	if err := json.Unmarshal([]byte(`+"`"+`{"value":{"type":"top","top":"18446744073709551615"}}`+"`"+`), &got); err != nil {
		t.Fatal(err)
	}
	if top, ok := got.Value.(*BoxValueTop); !ok || top.Top != 18446744073709551615 {
		t.Fatalf("unmarshal = %#v", got.Value)
	}
}
`)
}
