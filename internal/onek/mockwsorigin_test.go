package onek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestMockWebSocketAcceptsCrossOriginUnlessCORSIsOff(t *testing.T) {
	schema := `
package probe
message Frame { text: string }
service Live { chat(Frame) -> Frame @ws("/chat") }
`
	for _, tc := range []struct {
		noCORS bool
		ok     bool
	}{{false, true}, {true, false}} {
		dir := t.TempDir()
		writeTestFile(t, dir+"/live.onk", schema)
		server, err := NewMockServer(dir, MockOptions{NoCORS: tc.noCORS})
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(server.Handler())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/chat", &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{"http://localhost:5173"}},
		})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if tc.ok {
			if err != nil {
				t.Fatalf("cross-origin dial with CORS on: %v", err)
			}
			if _, _, err := conn.Read(ctx); err != nil {
				t.Fatalf("read fixture frame: %v", err)
			}
			_ = conn.CloseNow()
		} else if err == nil {
			_ = conn.CloseNow()
			t.Fatal("cross-origin dial succeeded with --no-cors")
		}
		cancel()
		srv.Close()
	}
}
