package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const fixtureSrc = `
package main

message User {
  id: string
  name: string @len(2, 100)
  email: string @email
}

message CreateUserRequest {
  name: string @len(2, 100)
  email: string @email
}

message GetUserRequest {
  id: string
}

message NotFoundError @status(404) {
  resource_type: string
  resource_id: string
}

message TextPayload {
  body: string
}

message ImagePayload {
  url: string
}

message SendMessageRequest {
  channel: string
  payload: oneof(discriminator: "type") {
    text: TextPayload
    image: ImagePayload
  }
}

message SendMessageResponse {
  message_id: string
}

service UserService {
  base_path: "/api/v1"

  createUser(CreateUserRequest) -> User @post("/users")

  getUser(GetUserRequest) -> User | NotFoundError @get("/users/{id}")

  sendMessage(SendMessageRequest) -> SendMessageResponse @post("/messages")
}
`

func compileFixture(t *testing.T) *onkir.File {
	t.Helper()
	ast, err := onklang.Parse(fixtureSrc)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
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
	if err := RegisterUserServiceServer(&impl{users: map[string]*User{}}, WithMux(mux)); err != nil {
		fail("register: %v", err)
	}
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
