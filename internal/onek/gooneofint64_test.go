package onek

import "testing"

func TestGoOneofInt64VariantsUseStrings(t *testing.T) {
	buildGoSchema(t, `namespace check;

union BoxValue {
  @Onkt.tag("big") big: int64,
  @Onkt.tag("top") top: uint64,
}

model Box {
  value: BoxValue;
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
