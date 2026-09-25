package onek

import "testing"

func TestGoClientPerCallOptions(t *testing.T) {
	buildGoSchema(t, `
package check

message Ping { id: string }

service Pings { get(Ping) -> Ping @get("/pings/{id}") }
`, `package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCallOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`+"`"+`{"id":"`+"`"+` + r.Header.Get("X-Request-Id") + r.URL.Query().Get("trace") + `+"`"+`"}`+"`"+`))
	}))
	defer srv.Close()
	client := NewPingsClient(srv.URL)
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b", "c", "d"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(context.Background(), &Ping{Id: "1"}, WithHeader("X-Request-Id", id), WithRequestEditor(func(r *http.Request) {
				q := r.URL.Query()
				q.Set("trace", "!")
				r.URL.RawQuery = q.Encode()
			}))
			if err != nil || resp.Id != id+"!" {
				t.Errorf("call %s: %v %+v", id, err, resp)
			}
		}()
	}
	wg.Wait()
}
`)
}
