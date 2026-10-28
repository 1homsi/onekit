package genpy

import "testing"

// A field of the wrong Python type is reported as "<field> has invalid type",
// and the field's other checks, which assume the declared type, are skipped,
// so validate() raises ValueError instead of TypeError or AttributeError.
func TestPythonValidateReportsAFieldOfTheWrongType(t *testing.T) {
	runPythonSchema(t, `namespace app;

enum Color { red, green }

model Guest {
  @minLength(1)
  @maxLength(50)
  name: string;
}

model Address {
  @minLength(1)
  @maxLength(80)
  city: string;
}

model SignIn {
  @format("email")
  @minLength(3)
  @maxLength(254)
  email: string;
  guests: Guest[];
  rooms: Record<Guest>;
  home?: Address;
  @minValue(1)
  party?: int32;
  @minValue(1)
  @encode(string)
  big?: int64;
  @minValue(0.5)
  ratio?: float64;
  flag?: boolean;
  color?: Color;
  blob?: bytes;
  at?: utcDateTime;
  @encode("unixTimestamp", int64)
  seen?: utcDateTime;
  tags: string[];
}
`, `
import json
from models import SignIn, Guest, Address, Color

def violations(req):
    try:
        req.validate()
    except ValueError as error:
        return str(error)
    return ""

ok = "guest@example.com"
cases = [
    (SignIn(email=ok), ""),
    (SignIn(email="x"), "email must be a valid email; email has invalid length"),
    (SignIn(email=12345), "email has invalid type"),
    (SignIn(email=True), "email has invalid type"),
    (SignIn(email={"a": 1}), "email has invalid type"),
    (SignIn(email=ok, guests=5), "guests has invalid type"),
    (SignIn(email=ok, guests="abc"), "guests has invalid type"),
    (SignIn(email=ok, guests={"name": "a"}), "guests has invalid type"),
    (SignIn(email=ok, guests=[{"name": "a"}]), "guests has invalid type"),
    (SignIn(email=ok, guests=[None]), "guests has invalid type"),
    (SignIn(email=ok, guests=[Guest(name=5)]), "guests: name has invalid type"),
    (SignIn(email=ok, guests=[Guest(name="")]), "guests: name has invalid length"),
    (SignIn(email=ok, rooms=5), "rooms has invalid type"),
    (SignIn(email=ok, rooms=[Guest(name="a")]), "rooms has invalid type"),
    (SignIn(email=ok, rooms={"a": None}), "rooms has invalid type"),
    (SignIn(email=ok, rooms={1: Guest(name="a")}), "rooms has invalid type"),
    (SignIn(email=ok, rooms={"a": Guest(name=5)}), "rooms: name has invalid type"),
    (SignIn(email=ok, home="x"), "home has invalid type"),
    (SignIn(email=ok, home=Guest(name="a")), "home has invalid type"),
    (SignIn(email=ok, home=Address(city=5)), "home: city has invalid type"),
    (SignIn(email=ok, home=Address(city="")), "home: city has invalid length"),
    (SignIn(email=ok, party=0), "party violates @gte"),
    (SignIn(email=ok, party="abc"), "party has invalid type"),
    (SignIn(email=ok, party=True), "party has invalid type"),
    (SignIn(email=ok, party=1.5), "party has invalid type"),
    (SignIn(email=ok, party=2**31), "party must be a int32"),
    (SignIn(email=ok, big=0), "big violates @gte"),
    (SignIn(email=ok, big="5"), "big has invalid type"),
    (SignIn(email=ok, big=True), "big has invalid type"),
    (SignIn(email=ok, ratio=0.25), "ratio violates @gte"),
    (SignIn(email=ok, ratio="1"), "ratio has invalid type"),
    (SignIn(email=ok, ratio=False), "ratio has invalid type"),
    (SignIn(email=ok, ratio=float("nan")), "ratio must be a finite number; ratio violates @gte"),
    (SignIn(email=ok, ratio=float("-inf")), "ratio must be a finite number; ratio violates @gte"),
    (SignIn(email=ok, flag=1), "flag has invalid type"),
    (SignIn(email=ok, color="red"), "color has invalid type"),
    (SignIn(email=ok, color=7), "color has invalid type"),
    (SignIn(email=ok, blob="abc"), "blob has invalid type"),
    (SignIn(email=ok, at=5), "at has invalid type"),
    (SignIn(email=ok, seen="2024-01-01T00:00:00Z"), "seen has invalid type"),
    (SignIn(email=ok, tags=["a", 5]), "tags has invalid type"),
    (SignIn(email=ok, tags="ab"), "tags has invalid type"),
]
for req, want in cases:
    got = violations(req)
    assert got == want, "%r: got %r, want %r" % (req, got, want)

good = SignIn(email=ok, guests=[Guest(name="a")], rooms={"a": Guest(name="b")}, home=Address(city="c"),
    party=2, big=2**62, ratio=1, flag=False, color=Color.green, blob=b"\x00", at="2024-01-01T00:00:00Z",
    seen=1700000000, tags=["x"])
assert violations(good) == "", violations(good)
assert violations(SignIn(email=ok, color=1)) == ""
json.dumps(good.to_dict())
print("OK")
`)
}
