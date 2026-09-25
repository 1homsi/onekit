package onek

import "testing"

func TestGoWebSocketPackageWithEmptyMessagesCompiles(t *testing.T) {
	buildGoSchema(t, `
package check

message PingRequest {}
message StatsRequest {}
message Stats { active: int32 }
message Frame {
  payload: oneof {
    ping: PingRequest @tag("ping")
    stats: Stats @tag("stats")
  }
}

service Live {
  stream(Frame) -> Frame @ws("/live")
  ping(PingRequest) -> PingRequest @ws("/ping")
  stats(StatsRequest) -> Stats @get("/stats")
}
`, `package api

import "testing"

func TestEmptyFramesRoundTrip(t *testing.T) {
	var ping PingRequest
	d := wsJSON{data: []byte(`+"`"+`{"extra":1}`+"`"+`)}
	ping.wsDecodeJSON(&d)
	if !d.end() {
		t.Fatal("fast decoder rejected an empty message with an unknown key")
	}
	var frame Frame
	if err := wsUnmarshal([]byte(`+"`"+`{"payload":{"type":"ping","ping":{}}}`+"`"+`), &frame); err != nil {
		t.Fatal(err)
	}
	if _, ok := frame.Payload.(*FramePayloadPing); !ok {
		t.Fatalf("payload = %#v", frame.Payload)
	}
}
`)
}
