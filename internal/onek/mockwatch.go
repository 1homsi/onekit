package onek

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

type mockReloader struct {
	dir      string
	opts     MockOptions
	handler  atomic.Pointer[http.Handler]
	snapshot []fileStamp
}

func newMockReloader(dir string, opts MockOptions) (*mockReloader, int, error) {
	server, err := NewMockServer(dir, opts)
	if err != nil {
		return nil, 0, err
	}
	snapshot, err := projectSnapshot(dir)
	if err != nil {
		return nil, 0, err
	}
	reloader := &mockReloader{dir: dir, opts: opts, snapshot: snapshot}
	handler := server.Handler()
	reloader.handler.Store(&handler)
	return reloader, server.Routes(), nil
}

func (r *mockReloader) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	(*r.handler.Load()).ServeHTTP(w, req)
}

func (r *mockReloader) reload(out io.Writer) {
	current, err := projectSnapshot(r.dir)
	if err != nil || sameSnapshot(r.snapshot, current) {
		return
	}
	r.snapshot = current
	server, err := NewMockServer(r.dir, r.opts)
	if err != nil {
		if out != nil {
			_, _ = fmt.Fprintf(out, "onekit mock: reload failed, still serving the previous schema: %v\n", err)
		}
		return
	}
	handler := server.Handler()
	r.handler.Store(&handler)
	if out != nil {
		_, _ = fmt.Fprintf(out, "onekit mock: reloaded %d route(s)\n", server.Routes())
	}
}

// RunMockWithReload serves the mock like MockServer.Run and rebuilds it
// whenever the project's schema or config changes.
func RunMockWithReload(ctx context.Context, dir string, opts MockOptions, interval time.Duration, out io.Writer) error {
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	reloader, routes, err := newMockReloader(dir, opts)
	if err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reloader.reload(out)
			}
		}
	}()
	return serveMock(ctx, opts.Addr, reloader, routes, out)
}
