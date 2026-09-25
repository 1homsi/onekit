package onek

import "testing"

func TestGoWSBindsPathAndQueryIntoFrames(t *testing.T) {
	buildGoSchema(t, `
package check

message Msg {
  room: string
  limit: int32? @query
  text: string @len(1, 100)
}

service Chat { chat(Msg) -> Msg @ws("/rooms/{room}") }
`, `package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type chat struct{}

func (chat) Chat(ctx context.Context, frame *Msg, out WSOut[Msg]) error {
	return out.Send(ctx, frame)
}

func TestBinding(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterChatServer(mux, chat{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	limit := int32(5)
	socket, err := NewChatClient(srv.URL).Chat(ctx, &Msg{Room: "lobby", Limit: &limit})
	if err != nil {
		t.Fatalf("connect rejected a request whose frame-only fields are empty: %v", err)
	}
	defer socket.Close()
	if err := socket.Send(ctx, &Msg{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	echo, err := socket.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if echo.Room != "lobby" || echo.Limit == nil || *echo.Limit != 5 || echo.Text != "hi" {
		t.Fatalf("bound values missing from frame: %+v", echo)
	}
}
`)
}
