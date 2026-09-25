package genpy

import "testing"

func TestPythonRequiredOneof(t *testing.T) {
	runPythonSchema(t, `
package app
message A { v: string }
message M { p: oneof { a: A } @required }
`, `
from models import M

try:
    M().validate()
    raise SystemExit("missing oneof passed")
except ValueError as error:
    assert "p is required" in str(error), error
from models import A, MPA
M(p=MPA(a=A(v="x"))).validate()
assert M.from_dict({"p": {"type": "a", "a": {"v": "x"}}}).p == MPA(a=A(v="x"))
print("OK")
`)
}
