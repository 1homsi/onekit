package genpy

import "testing"

func TestPythonOutputCarriesDocstrings(t *testing.T) {
	runPythonSchema(t, `
package app
/// A stored note.
///
/// Quotes """ and \ stay intact.
message Note {
  /// Unique identifier.
  id: string
}
/// Nothing inside.
message Empty {}
/// Visibility of a note.
enum Visibility { PRIVATE PUBLIC }
/// Raised when a note is missing.
message Missing @status(404) { id: string }
/// Manages notes.
service Notes {
  /// Fetch one note.
  get(Note) -> Note | Missing @get("/notes/{id}")
}
`, `
import inspect
from models import Empty, Missing, Note, Visibility
from client import NotesClient

assert inspect.getdoc(Note) == 'A stored note.\n\nQuotes """ and \\ stay intact.', repr(Note.__doc__)
assert Empty.__doc__ == "Nothing inside."
assert Visibility.__doc__ == "Visibility of a note."
assert Missing.__doc__ == "Raised when a note is missing."
assert NotesClient.__doc__ == "Manages notes."
assert NotesClient.get.__doc__ == "Fetch one note."
assert Note(id="x").to_dict() == {"id": "x"}
print("OK")
`)
}
