package genpy

import "testing"

func TestPythonKeepsFalsyJSONValues(t *testing.T) {
	runPythonSchema(t, `namespace app;

model M {
  data: unknown;
}
`, `
from models import M

for value in (False, 0, [], {}, ""):
    assert M(data=value).to_dict() == {"data": value}, value
assert "data" not in M(data=None).to_dict()
print("OK")
`)
}
