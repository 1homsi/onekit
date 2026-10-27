package gents

import (
	"strings"
	"testing"
)

const mswFixture = `namespace mswfix;

enum Plan {
  FREE,
  PRO,
}

@error
model NotFound {
  @statusCode _: 404;
  message: string;
}

model User {
  @format("uuid")
  id: string;
  @format("email")
  email: string;
  plan: Plan;
  @encode("unixTimestamp", int64)
  joined_at: utcDateTime;
  @Onkt.encode("hex")
  avatar: bytes;
}

model GetUserRequest {
  @format("uuid")
  id: string;
}

model Empty {}

@route("/v1")
interface UserService {
  @get
  @route("/users/{id}")
  getUser(...GetUserRequest): User;

  @Onkt.method("QUERY")
  @route("/users/search")
  search(...Empty): User;

  @get
  @route("/users/events")
  @Onkt.stream
  events(...Empty): User;
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
	file, err := compileForTest(`namespace empty;

model M {
  x: string;
}
`)
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

func TestMSWStreamFramesAreNamedAfterTheOneofVariant(t *testing.T) {
	file, err := compileForTest(`namespace app;

model Text {
  text: string;
}

model Done {
  reason: string;
}

@Onkt.discriminator("type")
union TurnPayload {
  @Onkt.tag("text") text: Text,
  @Onkt.tag("done") done: Done,
}

model Turn {
  payload: TurnPayload;
}

model TurnRequest {
  prompt: string;
}

interface Agent {
  @post
  @route("/turn")
  @Onkt.stream
  turn(...TurnRequest): Turn;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := string(GenerateMSWHandlersWithResolver(file, nil))
	if !strings.Contains(out, `"event: text\ndata: "`) {
		t.Fatalf("stream frame must be named after the first variant:\n%s", out)
	}
}
