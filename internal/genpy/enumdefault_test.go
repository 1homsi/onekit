package genpy

import "testing"

func TestPythonRequiredEnumDefaultsToFirstValue(t *testing.T) {
	runPythonSchema(t, `namespace app;

enum Status {
  UNKNOWN,
  ACTIVE,
}

model M {
  status: Status;
  maybe?: Status;
}

@Onkt.parent(M)
model Inner {
  kind: Kind;
}

@Onkt.parent(Inner)
enum Kind {
  A,
  B,
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
