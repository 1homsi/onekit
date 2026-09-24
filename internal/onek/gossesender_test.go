package onek

import "testing"

func TestGoSSESenderReportsDisconnectsAndSerializesSends(t *testing.T) {
	buildGoSchema(t, `
package check

message WatchRequest { id: string }
message Tick { n: int32 }

service Feed { watch(WatchRequest) -> Tick @get("/feed/{id}") @stream }
`, `package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type feed struct{ done chan error }

func (f feed) Watch(ctx context.Context, req *WatchRequest, out SSESender) error {
	if req.Id == "burst" {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					_ = out.Send(&Tick{N: int32(j)})
				}
			}()
		}
		wg.Wait()
		return nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := out.Send(&Tick{N: 1}); err != nil {
			f.done <- err
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	f.done <- nil
	return nil
}

func TestSender(t *testing.T) {
	f := feed{done: make(chan error, 1)}
	mux := http.NewServeMux()
	if err := RegisterFeedServer(mux, f); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/feed/burst")
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(resp.Body)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: {") || !strings.HasSuffix(line, "}") {
			t.Fatalf("interleaved frame: %q", line)
		}
		count++
	}
	if count != 400 {
		t.Fatalf("want 400 events, got %d", count)
	}

	ctx, cancel := context.WithCancel(context.Background())
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/feed/drop", nil)
	resp, err = http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = bufio.NewReader(resp.Body).ReadString('\n')
	cancel()
	resp.Body.Close()
	if err := <-f.done; err == nil {
		t.Fatal("Send kept succeeding after the client disconnected")
	}
}
`)
}
