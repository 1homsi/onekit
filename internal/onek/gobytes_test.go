package onek

import "testing"

func TestGoBase64BytesEncodingCompiles(t *testing.T) {
	buildGoSchema(t, `namespace check;

model Blob {
  @Onkt.encode("base64")
  data: bytes;
  @Onkt.encode("base64")
  maybe?: bytes;
}
`, `package api

import (
	"encoding/json"
	"testing"
)

func TestBase64(t *testing.T) {
	data, err := json.Marshal(&Blob{Data: []byte{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `+"`"+`{"data":"AQID"}`+"`"+` {
		t.Fatalf("unexpected encoding %s", data)
	}
}
`)
}
