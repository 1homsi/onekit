package onek

import "testing"

func TestGoSSEClientFollowsEventStreamFraming(t *testing.T) {
	buildGoSchema(t, `
package check

message WatchRequest { id: string }
message Tick { n: int32  text: string }

service Feed { watch(WatchRequest) -> Tick @get("/feed/{id}") @stream }
`, `package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFraming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": keepalive\n\nevent: tick\n\ndata: {\"n\":1,\n")
		_, _ = io.WriteString(w, "data: \"text\":\"a\"}\r\n\r\nid: 7\ndata: {\"n\":2}\n\n")
	}))
	defer srv.Close()
	stream, err := NewFeedClient(srv.URL).Watch(context.Background(), &WatchRequest{Id: "1"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var got []Tick
	var tick Tick
	for stream.Next(&tick) {
		got = append(got, tick)
		tick = Tick{}
	}
	if stream.Err() != nil {
		t.Fatal(stream.Err())
	}
	if len(got) != 2 || got[0].N != 1 || got[0].Text != "a" || got[1].N != 2 {
		t.Fatalf("unexpected events %+v", got)
	}
}
`)
}
