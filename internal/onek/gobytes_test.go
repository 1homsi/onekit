package onek

import "testing"

func TestGoBase64BytesEncodingCompiles(t *testing.T) {
	buildGoSchema(t, `
package check

message Blob {
  data: bytes @encode(base64)
  maybe: bytes? @encode(base64)
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
