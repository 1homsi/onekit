package gents

import (
	"strings"
	"testing"
)

const mswFixture = `
package mswfix

enum Plan { FREE PRO }
message NotFound @status(404) { message: string }

message User {
  id: string @uuid
  email: string @email
  plan: Plan
  joined_at: timestamp @encode(unix_seconds)
  avatar: bytes @encode(hex)
}

message GetUserRequest { id: string @uuid }
message Empty {}

service UserService {
  base_path: "/v1"

  getUser(GetUserRequest) -> User @get("/users/{id}")
  search(Empty) -> User @query("/users/search")
  events(Empty) -> User @get("/users/events") @stream
}
`

func TestGenerateMSWHandlers(t *testing.T) {
	file, err := compileForTest(mswFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateMSWHandlersWithResolver(file, nil))
	for _, want := range []string{
		`import { http, HttpResponse } from "msw";`,
		`export const UserServiceHandlers = [`,
		// {param} -> :param conversion
		`http.get("/v1/users/:id", () => HttpResponse.json(`,
		// validator-derived fixture values
		`"0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f"`,
		`"user@example.com"`,
		`"plan": "FREE"`,
		`"joined_at": 1735689600`,
		`"avatar": "61"`,
		// custom QUERY method falls back to http.all with a method guard
		`http.all("/v1/users/search", ({ request }) => {`,
		`if (request.method !== "QUERY") return undefined;`,
		// SSE served as event-stream frames
		`"Content-Type": "text/event-stream"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated msw.ts missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateMSWNilWithoutServices(t *testing.T) {
	file, err := compileForTest("package empty\n\nmessage M { x: string }\n")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if got := GenerateMSWHandlersWithResolver(file, nil); got != nil {
		t.Fatalf("expected nil output without services, got:\n%s", got)
	}
}

func TestMSWPathPattern(t *testing.T) {
	if got := mswPathPattern("/v1/users/{id}/posts/{post_id}"); got != "/v1/users/:id/posts/:post_id" {
		t.Fatalf("mswPathPattern = %q", got)
	}
}
