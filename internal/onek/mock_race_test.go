package onek

import (
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// MockServer.handle is an http.HandlerFunc, so net/http runs every request in
// its own goroutine. m.rng is a single *rand.Rand shared by all routes, and
// math/rand/v2 documents that a *Rand must be used by a single goroutine at a
// time. Two overlapping requests therefore corrupt the PCG state, which also
// silently breaks the seeded-determinism guarantee.
//
// Run under -race to observe the detector report.
func TestMockServerConcurrentDrawsDoNotRace(t *testing.T) {
	srv := newMockTestServer(t, MockOptions{
		Seed:      7,
		Latency:   time.Microsecond,
		ErrorRate: 0.5,
	})

	const workers = 16
	const rounds = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range rounds {
				resp, err := http.Get(srv.URL + "/v1/things/1")
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
}
