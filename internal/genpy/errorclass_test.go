package genpy

import "testing"

func TestPythonErrorClassDefaultsAndEquality(t *testing.T) {
	runPythonSchema(t, `
package app
message Detail { field: string }
message InvalidError @status(400) {
  message: string
  details: Detail[]
  meta: map[string, string]
}
`, `
from models import InvalidError

error = InvalidError()
assert error.details == [] and error.meta == {} and error.message == "", vars(error)
assert InvalidError(message="x") == InvalidError.from_dict({"message": "x"})
assert InvalidError(message="x") != InvalidError(message="y")
print("OK")
`)
}
