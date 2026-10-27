package onek

import "testing"

func TestGoFlattenedOneofMergesVariantFieldsOnTheWire(t *testing.T) {
	buildGoSchema(t, `namespace check;

model Email {
  address: string;
}

model Phone {
  number: string;
}

@discriminated(#{ discriminatorPropertyName: "kind", envelope: "none" })
union ContactVia {
  @Onkt.tag("email") email: Email,
  @Onkt.tag("phone") phone: Phone,
}

model Contact {
  via: ContactVia;
}
`, `package api

import (
	"encoding/json"
	"testing"
)

func TestFlattenedOneof(t *testing.T) {
	data, err := json.Marshal(&Contact{Via: &ContactViaEmail{Email: &Email{Address: "a@b"}}})
	if err != nil || string(data) != `+"`"+`{"via":{"kind":"email","address":"a@b"}}`+"`"+` {
		t.Fatalf("marshal = %s, %v", data, err)
	}
	var got Contact
	if err := json.Unmarshal([]byte(`+"`"+`{"via":{"kind":"phone","number":"555"}}`+"`"+`), &got); err != nil {
		t.Fatal(err)
	}
	phone, ok := got.Via.(*ContactViaPhone)
	if !ok || phone.Phone.Number != "555" {
		t.Fatalf("unmarshal = %#v", got.Via)
	}
	data, _ = json.Marshal(&Contact{Via: &ContactViaPhone{}})
	if string(data) != `+"`"+`{"via":{"kind":"phone"}}`+"`"+` {
		t.Fatalf("empty variant = %s", data)
	}
}
`)
}
