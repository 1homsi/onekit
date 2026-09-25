package genpy

import "testing"

func TestPythonUUIDMatchesGoFormat(t *testing.T) {
	runPythonSchema(t, `
package app
message Ref { id: string @uuid }
`, `
from models import Ref

Ref(id="0f8fad5b-d9cb-469f-a165-70867728950e").validate()
for value in ("{0f8fad5b-d9cb-469f-a165-70867728950e}", "urn:uuid:0f8fad5b-d9cb-469f-a165-70867728950e", "0f8fad5bd9cb469fa16570867728950e"):
    try:
        Ref(id=value).validate()
        raise SystemExit("accepted non-canonical UUID: " + value)
    except ValueError:
        pass
print("OK")
`)
}
