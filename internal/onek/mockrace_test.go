package onek

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestMockServerHandlesConcurrentRequests(t *testing.T) {
	ts := newMockTestServer(t, MockOptions{Seed: 7, ErrorRate: 0.5, Latency: time.Millisecond})
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(ts.URL + "/v1/things/abc")
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
				errs <- fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
