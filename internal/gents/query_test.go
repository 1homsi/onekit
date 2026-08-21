package gents

import (
	"strings"
	"testing"
)

const reactQueryFixture = `
package rqfix

message GetUserRequest { id: string @uuid }
message User {
  id: string
  email: string @email
  last_seen: timestamp @encode(unix_millis)
}
message CreateUserRequest { email: string @email }
message Tick { seq: int64 }

service UserService {
  base_path: "/v1"

  getUser(GetUserRequest) -> User @get("/users/{id}")
  createUser(CreateUserRequest) -> User @post("/users")
  watchTicks(GetUserRequest) -> Tick @get("/ticks") @stream
}
`

func TestGenerateReactQueryHooks(t *testing.T) {
	file, err := compileForTest(reactQueryFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateReactQueryWithResolver(file, nil))
	for _, want := range []string{
		`import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";`,
		`export function createUserServiceHooks(client: UserServiceClient) {`,
		// GET -> useQuery with a stable composite key
		`useGetUser(req: GetUserRequest, opts?: { enabled?: boolean }) {`,
		`queryKey: ["UserService", "getUser", req],`,
		`queryFn: () => client.getUser(req),`,
		// POST -> useMutation invalidating the service scope
		`useCreateUser() {`,
		`mutationFn: (req: CreateUserRequest) => client.createUser(req),`,
		`queryClient.invalidateQueries({ queryKey: ["UserService"] });`,
		// SSE -> resilient stream hook wired to the async generator client
		`useWatchTicks(req: GetUserRequest, opts?: { enabled?: boolean; retries?: number }): SchemaStreamResult<Tick> {`,
		`subscribe: (signal) => client.watchTicks(req, { signal }),`,
		`function useSchemaStream<T>(options: SchemaStreamOptions<T>): SchemaStreamResult<T> {`,
		`await new Promise((resolve) => setTimeout(resolve, Math.min(30000, 500 * 2 ** (attempt - 1))));`,
		`export function isApiError(err: unknown): err is ApiError {`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated query.ts missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateReactQuerySkipsTanStackWhenOnlyStreams(t *testing.T) {
	src := `
package streamonly

message Ping {}
message Pong {}

service S {
  base_path: "/s"
  ticks(Ping) -> Pong @get("/t") @stream
}
`
	file, err := compileForTest(src)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateReactQueryWithResolver(file, nil))
	if strings.Contains(text, "@tanstack/react-query") {
		t.Fatalf("stream-only service must not import TanStack Query:\n%s", text)
	}
	if !strings.Contains(text, "useSchemaStream<Pong>(") {
		t.Fatalf("stream hook missing:\n%s", text)
	}
}

func TestGenerateReactQueryNilWithoutServices(t *testing.T) {
	file, err := compileForTest("package empty\n\nmessage M { x: string }\n")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if got := GenerateReactQueryWithResolver(file, nil); got != nil {
		t.Fatalf("expected nil output without services, got:\n%s", got)
	}
}
