package gents

import "testing"

func TestTSFlattenedOneofMergesVariantFieldsOnTheWire(t *testing.T) {
	runTSSchema(t, `
package app
message Email { address: string }
message Phone { number: string }
message Contact {
  via: oneof(discriminator: "kind", flatten: true) {
    email: Email @tag("email")
    phone: Phone @tag("phone")
  }
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
