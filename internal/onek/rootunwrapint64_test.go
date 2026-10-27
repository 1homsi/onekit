package onek

import "testing"

func TestGoRootUnwrapInt64UsesStrings(t *testing.T) {
	buildGoSchema(t, `namespace check;

model Ids {
  @Onkt.unwrap
  @Onkt.encode("string")
  v: int64[];
}

model Total {
  @Onkt.unwrap
  @encode(string)
  v: uint64;
}

model Count {
  @Onkt.unwrap
  v: int64;
}
`, `package api

import (
	"encoding/json"
	"testing"
)

func TestRootUnwrapInt64(t *testing.T) {
	data, err := json.Marshal(&Ids{V: []int64{1, -9007199254740993}})
	if err != nil || string(data) != `+"`"+`["1","-9007199254740993"]`+"`"+` {
		t.Fatalf("ids = %s, %v", data, err)
	}
	var ids Ids
	if err := json.Unmarshal(data, &ids); err != nil || ids.V[1] != -9007199254740993 {
		t.Fatalf("decoded %v, %v", ids.V, err)
	}
	data, _ = json.Marshal(&Total{V: 18446744073709551615})
	if string(data) != `+"`"+`"18446744073709551615"`+"`"+` {
		t.Fatalf("total = %s", data)
	}
	data, _ = json.Marshal(&Count{V: 3})
	if string(data) != "3" {
		t.Fatalf("counts = %s", data)
	}
}
`)
}
