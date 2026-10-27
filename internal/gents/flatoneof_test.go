package gents

import "testing"

func TestTSFlattenedOneofMergesVariantFieldsOnTheWire(t *testing.T) {
	runTSSchema(t, `namespace app;

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
`, `
import { decodeContact, encodeContact } from "./types.ts";

const wire = JSON.stringify(encodeContact({ via: { kind: "email", address: "a@b" } }));
if (wire !== '{"via":{"address":"a@b","kind":"email"}}') throw new Error("encoded " + wire);
const back = decodeContact(JSON.parse('{"via":{"kind":"phone","number":"555"}}'));
if (back.via?.kind !== "phone" || (back.via as any).number !== "555") throw new Error("decoded " + JSON.stringify(back));
console.log("OK");
`)
}
