package onek

import "testing"

func TestGoStreamWithPathParamCompiles(t *testing.T) {
	buildGoSchema(t, `
package check

message WatchRequest { room: string }
message Tick { n: int32 }

service Rooms { watch(WatchRequest) -> Tick @get("/rooms/{room}/events") @stream }
`, "")
}

func TestGoStreamSendsWrappedTypedErrors(t *testing.T) {
	buildGoSchema(t, `
package check

message WatchRequest { room: string }
message Tick { n: int32 }
message Gone @status(410) { reason: string }

service Rooms { watch(WatchRequest) -> Tick | Gone @get("/rooms/{room}/events") @stream }
`, `package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rooms struct{}

func (rooms) Watch(ctx context.Context, req *WatchRequest, out SSESender) error {
	if err := out.Send(&Tick{N: 1}); err != nil {
		return err
	}
	return fmt.Errorf("closing: %w", &Gone{Reason: "bye"})
}

func TestWrappedStreamError(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterRoomsServer(mux, rooms{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/rooms/a/events")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "event: error\ndata: {\"reason\":\"bye\"}") {
		t.Fatalf("wrapped typed error not sent: %s", body)
	}
}
`)
}
