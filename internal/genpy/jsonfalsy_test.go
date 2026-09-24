package genpy

import "testing"

func TestPythonKeepsFalsyJSONValues(t *testing.T) {
	runPythonSchema(t, `
package app
message M { data: json }
`, `
from models import M

for value in (False, 0, [], {}, ""):
    assert M(data=value).to_dict() == {"data": value}, value
assert "data" not in M(data=None).to_dict()
print("OK")
`)
}
