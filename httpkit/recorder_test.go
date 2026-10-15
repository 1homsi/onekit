package httpkit

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMiddlewareKeepsHijackForUpgrades(t *testing.T) {
	handler := Middleware(Config{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijacker", http.StatusTeapot)
			return
		}
		conn, rw, err := hijacker.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusTeapot)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: test\r\nConnection: Upgrade\r\n\r\nhello")
		_ = rw.Flush()
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
	raw, _ := io.ReadAll(bufio.NewReader(conn))
	if !strings.Contains(string(raw), "101 Switching Protocols") || !strings.HasSuffix(string(raw), "hello") {
		t.Fatalf("an upgrade behind Middleware failed: %q", raw)
	}
}

func TestRecorderTreatsInformationalStatusesAsInterim(t *testing.T) {
	var recorded int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := NewRecorder(w)
		rec.Header().Set("Link", "</style.css>; rel=preload")
		rec.WriteHeader(http.StatusEarlyHints)
		rec.WriteHeader(http.StatusNotFound)
		_, _ = rec.Write([]byte("missing"))
		recorded = rec.Status()
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound || string(body) != "missing" {
		t.Fatalf("the final status was lost behind a 103: %d %q", resp.StatusCode, body)
	}
	if recorded != http.StatusNotFound {
		t.Fatalf("the recorder kept %d, want 404", recorded)
	}
}
