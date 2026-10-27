package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onktest"
)

const fixtureSrc = `namespace main;

model User {
  id: string;
  @minLength(2)
  @maxLength(100)
  name: string;
  @format("email")
  email: string;
}

model CreateUserRequest {
  @minLength(2)
  @maxLength(100)
  name: string;
  @format("email")
  email: string;
}

model GetUserRequest {
  id: string;
}

@error
model NotFoundError {
  @statusCode _: 404;
  resource_type: string;
  resource_id: string;
}

model TextPayload {
  body: string;
}

model ImagePayload {
  url: string;
}

@Onkt.discriminator("type")
union SendMessageRequestPayload {
  text: TextPayload,
  image: ImagePayload,
}

model SendMessageRequest {
  channel: string;
  payload: SendMessageRequestPayload;
}

model SendMessageResponse {
  message_id: string;
}

@route("/api/v1")
interface UserService {
  @post
  @route("/users")
  createUser(...CreateUserRequest): User;

  @get
  @route("/users/{id}")
  getUser(...GetUserRequest): User | NotFoundError;

  @post
  @route("/messages")
  sendMessage(...SendMessageRequest): SendMessageResponse;
}
`

func compileFixture(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onktest.Parse(fixtureSrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	return pkg.Files[0]
}

func TestGenerateTypesFormatsCleanly(t *testing.T) {
	file := compileFixture(t)
	out, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("GenerateTypes error: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty generated types")
	}
}

func TestGenerateJSONScalarUsesNativeWireTypes(t *testing.T) {
	ast, err := onktest.Parse(`namespace jsonfixture;

model Envelope {
  payload: unknown;
  values: unknown[];
}
`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "json.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	out, err := GenerateTypes(pkg.Files[0])
	if err != nil {
		t.Fatalf("GenerateTypes error: %v", err)
	}
	text := string(out)
	for _, want := range []string{`"encoding/json"`, "Payload json.RawMessage", "[]json.RawMessage"} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated JSON scalar types missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateValidationFormatsCleanly(t *testing.T) {
	file := compileFixture(t)
	out, err := GenerateValidation(file)
	if err != nil {
		t.Fatalf("GenerateValidation error: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty generated validation code")
	}
}

func TestGenerateServerFormatsCleanly(t *testing.T) {
	file := compileFixture(t)
	out, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("GenerateServer error: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty generated server code")
	}
}

func TestGenerateServerIncludesRuntimeHooks(t *testing.T) {
	file := compileFixture(t)
	out, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("GenerateServer error: %v", err)
	}
	for _, want := range []string{
		"type Middleware func(http.Handler) http.Handler",
		"func WithRequestID(headerName string) ServerOption",
		"func WithAuthorizer(authorizer Authorizer) ServerOption",
		"func RequestMetadataFromContext(ctx context.Context)",
		"func WithRequestObserver(observer RequestObserver) ServerOption",
		"func (o ServerOptions) WriteHandlerError(w http.ResponseWriter, r *http.Request, err error)",
		"func WithErrorWriter(write ErrorWriter) ServerOption",
		"func ErrorHandler(next http.Handler, opts ...ServerOption) http.Handler",
		"errors.As(err, &statusErr)",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("generated server missing %q:\n%s", want, out)
		}
	}
}

func TestGenerateClientFormatsCleanly(t *testing.T) {
	file := compileFixture(t)
	out, err := GenerateClient(file)
	if err != nil {
		t.Fatalf("GenerateClient error: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatalf("expected non-empty generated client code")
	}
}

func TestGenerateClientEncodesCustomBodyFields(t *testing.T) {
	ast, err := onktest.Parse(`namespace bodyfixture;

enum State {
  UNKNOWN,
  READY,
}

model AmountRequest {
  @body
  @encode(string)
  amount: int64;
}

model PayloadRequest {
  @body
  @Onkt.encode("hex")
  payload: bytes;
}

model StateRequest {
  @body
  @Onkt.encode("number")
  state: State;
}

model Ack {
  ok: boolean;
}

interface BodyService {
  @post
  @route("/amount")
  sendAmount(...AmountRequest): Ack;

  @post
  @route("/payload")
  sendPayload(...PayloadRequest): Ack;

  @post
  @route("/state")
  sendState(...StateRequest): Ack;
}
`)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "body.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	out, err := GenerateClient(pkg.Files[0])
	if err != nil {
		t.Fatalf("GenerateClient error: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{
		`bodyValue = strconv.FormatInt(req.Amount, 10)`,
		`bodyValue = hex.EncodeToString(req.Payload)`,
		`bodyValue = int32(req.State)`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated body client missing %q:\n%s", want, text)
		}
	}
}

const harnessMain = `
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
)

type impl struct {
	users map[string]*User
}

func (s *impl) CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error) {
	u := &User{Id: fmt.Sprintf("user-%d", len(s.users)+1), Name: req.Name, Email: req.Email}
	s.users[u.Id] = u
	return u, nil
}

func (s *impl) GetUser(ctx context.Context, req *GetUserRequest) (*User, error) {
	u, ok := s.users[req.Id]
	if !ok {
		return nil, &NotFoundError{ResourceType: "user", ResourceId: req.Id}
	}
	return u, nil
}

func (s *impl) SendMessage(ctx context.Context, req *SendMessageRequest) (*SendMessageResponse, error) {
	switch v := req.Payload.(type) {
	case *SendMessageRequestPayloadText:
		return &SendMessageResponse{MessageId: "text:" + v.Text.Body}, nil
	case *SendMessageRequestPayloadImage:
		return &SendMessageResponse{MessageId: "image:" + v.Image.Url}, nil
	default:
		return nil, fmt.Errorf("no payload set")
	}
}

func fail(msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(1)
}

func main() {
	mux := http.NewServeMux()
	RegisterUserServiceServer(mux, &impl{users: map[string]*User{}})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	client := NewUserServiceClient(srv.URL)

	created, err := client.CreateUser(ctx, &CreateUserRequest{Name: "Ada Lovelace", Email: "ada@example.com"})
	if err != nil {
		fail("create request failed: %v", err)
	}
	if created.Name != "Ada Lovelace" || created.Email != "ada@example.com" || created.Id == "" {
		fail("unexpected created user: %+v", created)
	}

	fetched, err := client.GetUser(ctx, &GetUserRequest{Id: created.Id})
	if err != nil {
		fail("get request failed: %v", err)
	}
	if fetched.Id != created.Id || fetched.Name != created.Name {
		fail("unexpected fetched user: %+v", fetched)
	}

	_, err = client.GetUser(ctx, &GetUserRequest{Id: "does-not-exist"})
	if err == nil {
		fail("expected error for missing user, got nil")
	}
	notFound, ok := err.(*NotFoundError)
	if !ok {
		fail("expected *NotFoundError, got %T: %v", err, err)
	}
	if notFound.ResourceType != "user" || notFound.ResourceId != "does-not-exist" {
		fail("unexpected not-found body: %+v", notFound)
	}

	_, err = client.CreateUser(ctx, &CreateUserRequest{Name: "A", Email: "not-an-email"})
	if err == nil {
		fail("expected error for invalid create, got nil")
	}

	textMsg, err := client.SendMessage(ctx, &SendMessageRequest{
		Channel: "general",
		Payload: &SendMessageRequestPayloadText{Text: &TextPayload{Body: "hello"}},
	})
	if err != nil {
		fail("SendMessage(text) failed: %v", err)
	}
	if textMsg.MessageId != "text:hello" {
		fail("unexpected text message result: %+v", textMsg)
	}

	imageMsg, err := client.SendMessage(ctx, &SendMessageRequest{
		Channel: "general",
		Payload: &SendMessageRequestPayloadImage{Image: &ImagePayload{Url: "https://example.com/x.png"}},
	})
	if err != nil {
		fail("SendMessage(image) failed: %v", err)
	}
	if imageMsg.MessageId != "image:https://example.com/x.png" {
		fail("unexpected image message result: %+v", imageMsg)
	}

	fmt.Println("OK")
}
`

func TestGeneratedServerBuildsAndServes(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	file := compileFixture(t)

	typesSrc, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("GenerateTypes error: %v", err)
	}
	validateSrc, err := GenerateValidation(file)
	if err != nil {
		t.Fatalf("GenerateValidation error: %v", err)
	}
	serverSrc, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("GenerateServer error: %v", err)
	}
	clientSrc, err := GenerateClient(file)
	if err != nil {
		t.Fatalf("GenerateClient error: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_gengo_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "types.go"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "validate.go"), string(validateSrc))
	writeFile(t, filepath.Join(dir, "server.go"), string(serverSrc))
	writeFile(t, filepath.Join(dir, "client.go"), string(clientSrc))
	writeFile(t, filepath.Join(dir, "main.go"), harnessMain)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}

func TestGeneratedServerPreservesRuntimeHTTPStatus(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	file := compileFixture(t)
	typesSrc, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("GenerateTypes error: %v", err)
	}
	validateSrc, err := GenerateValidation(file)
	if err != nil {
		t.Fatalf("GenerateValidation error: %v", err)
	}
	serverSrc, err := GenerateServer(file)
	if err != nil {
		t.Fatalf("GenerateServer error: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_gengo_status_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "types.go"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "validate.go"), string(validateSrc))
	writeFile(t, filepath.Join(dir, "server.go"), string(serverSrc))
	writeFile(t, filepath.Join(dir, "main.go"), `
package main

import (
  "context"
  "fmt"
  "net/http"
  "net/http/httptest"
  "os"
  "strings"
)

type statusError struct{}
func (statusError) Error() string { return "access denied" }
func (statusError) HTTPStatusCode() int { return http.StatusForbidden }

type impl struct{}
func (impl) CreateUser(context.Context, *CreateUserRequest) (*User, error) { return nil, statusError{} }
func (impl) GetUser(context.Context, *GetUserRequest) (*User, error) { return &User{}, nil }
func (impl) SendMessage(context.Context, *SendMessageRequest) (*SendMessageResponse, error) { return &SendMessageResponse{}, nil }

func main() {
  mux := http.NewServeMux()
  if err := RegisterUserServiceServer(mux, impl{}); err != nil { panic(err) }
  req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`+"`"+`{"name":"Alice","email":"alice@example.com"}`+"`"+`))
  res := httptest.NewRecorder()
  mux.ServeHTTP(res, req)
  if res.Code != http.StatusForbidden { fmt.Printf("got status %d\n", res.Code); os.Exit(1) }
  fmt.Println("OK")
}
`)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}

func TestGeneratedServerRejectsInvalidScalarBindings(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	ast, err := onktest.Parse(`namespace main;

model Request {
  @encode(string)
  id: int64;
  @query
  page: int32;
}

model Response {
  ok: boolean;
}

interface API {
  @get
  @route("/items/{id}")
  get(...Request): main.Response;
}
`)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	types, err := GenerateTypes(pkg.Files[0])
	if err != nil {
		t.Fatalf("generate types: %v", err)
	}
	server, err := GenerateServer(pkg.Files[0])
	if err != nil {
		t.Fatalf("generate server: %v", err)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_scalar_binding_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "types.go"), string(types))
	writeFile(t, filepath.Join(dir, "server.go"), string(server))
	writeFile(t, filepath.Join(dir, "main.go"), `
package main
import (
  "context"
  "fmt"
  "net/http"
  "net/http/httptest"
  "os"
)
type impl struct{}
func (impl) Get(context.Context, *Request) (*Response, error) { return &Response{Ok: true}, nil }
func main() {
  mux := http.NewServeMux()
  RegisterAPIServer(mux, impl{})
  s := httptest.NewServer(mux); defer s.Close()
  for _, path := range []string{"/items/not-a-number", "/items/1?page=also-bad"} {
    r, err := http.Get(s.URL + path); if err != nil { panic(err) }
    if r.StatusCode != http.StatusBadRequest { fmt.Printf("%s: %d\n", path, r.StatusCode); os.Exit(1) }
    _ = r.Body.Close()
  }
  fmt.Println("OK")
}`)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
