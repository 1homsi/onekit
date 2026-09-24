package genpy

import "testing"

func TestPythonFieldsNamedLikeDataclassHelpers(t *testing.T) {
	runPythonSchema(t, `
package app
message Violation {
  field: string
  list: string
  dict: string
  tags: string[]
  labels: map[string, string]
}
`, `
from models import Violation

v = Violation(field="email", list="a", dict="b")
assert v.tags == [] and v.labels == {}, v
assert Violation.from_dict(v.to_dict()) == v
print("OK")
`)
}
