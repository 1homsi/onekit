package genpy

import "testing"

func TestPythonRequiredEnumDefaultsToFirstValue(t *testing.T) {
	runPythonSchema(t, `
package app
enum Status { UNKNOWN  ACTIVE }
message M {
  status: Status
  maybe: Status?
  message Inner {
    kind: Kind
    enum Kind { A  B }
  }
}
`, `
from models import Inner, Kind, M, Status

assert M().status == Status.UNKNOWN, M()
assert M.from_dict({}).status == Status.UNKNOWN
assert M.from_dict({}).maybe is None
assert M.from_dict({"status": "ACTIVE"}).status == Status.ACTIVE
assert Inner().kind == Kind.A
print("OK")
`)
}
