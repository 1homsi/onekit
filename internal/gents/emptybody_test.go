package gents

import (
	"strings"
	"testing"
)

const emptyBodyFixture = `
package ebfix

message DeleteThingRequest { id: string }
message DeleteThingResponse { message: string }

service ThingService {
  base_path: "/v1"

  deleteThing(DeleteThingRequest) -> DeleteThingResponse @delete("/things/{id}")
}
`

// A 204 No Content, or any success with an empty body, must not reach
// JSON.parse. Unguarded it throws SyntaxError("Unexpected end of JSON input"),
// which consumers see as an opaque failure for an operation that succeeded.
//
// The error path in writeClientErrorHandling already guards this exact hazard,
// with a comment saying a non-JSON body must degrade rather than surface a raw
// SyntaxError. The success path did not.
func TestClientGuardsEmptySuccessBody(t *testing.T) {
	file, err := compileForTest(emptyBodyFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	out := string(GenerateClient(file))

	// The EXACT emitted line, not a substring. A substring assertion passed an
	// earlier attempt of this fix that emitted one closing paren too many, which
	// only surfaced when a consumer tried to parse the generated file.
	for _, want := range []string{
		"const text = await readResponseText(res, this.options.maxResponseBodyBytes);",
		`return decodeDeleteThingResponse(text.trim() === "" ? {} : JSON.parse(text));`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("emitted client missing %q\n---\n%s", want, out)
		}
	}

	// Parens must balance across the whole emitted client: the failure mode this
	// guards is a generator that emits syntactically invalid TypeScript, which no
	// substring assertion can see.
	if strings.Count(out, "(") != strings.Count(out, ")") {
		t.Errorf("emitted client has unbalanced parens: %d open, %d close",
			strings.Count(out, "("), strings.Count(out, ")"))
	}

	// And the unguarded form must be gone.
	if strings.Contains(out, "JSON.parse(await readResponseText(") {
		t.Errorf("emitted client still parses the success body unconditionally\n---\n%s", out)
	}
}
