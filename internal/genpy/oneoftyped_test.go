package genpy

import "testing"

func TestPythonOneofsAreTypedVariants(t *testing.T) {
	runPythonSchema(t, `
package app
enum Level { LOW HIGH }
message Email { address: string @email }
message Payload {
  value: oneof(discriminator: "kind") {
    email: Email @tag("email")
    blob: bytes @tag("blob")
    count: int64 @tag("count")
    level: Level @tag("level")
  }
}
`, `
from models import Email, Level, Payload, PayloadValueBlob, PayloadValueCount, PayloadValueEmail, PayloadValueLevel

cases = [
    (PayloadValueEmail(email=Email(address="a@b.io")), {"kind": "email", "email": {"address": "a@b.io"}}),
    (PayloadValueBlob(blob=b"\x01\x02"), {"kind": "blob", "blob": "AQI="}),
    (PayloadValueCount(count=9007199254740993), {"kind": "count", "count": "9007199254740993"}),
    (PayloadValueLevel(level=Level.HIGH), {"kind": "level", "level": "HIGH"}),
]
for value, wire in cases:
    assert Payload(value=value).to_dict() == {"value": wire}, Payload(value=value).to_dict()
    assert Payload.from_dict({"value": wire}).value == value, wire
assert Payload.from_dict({"value": {"kind": "unknown"}}).value is None
try:
    Payload(value=PayloadValueEmail(email=Email(address="nope"))).validate()
    raise SystemExit("invalid variant passed validation")
except ValueError as error:
    assert "value: " in str(error), error
print("OK")
`)
}
