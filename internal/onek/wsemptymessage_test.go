package onek

import "testing"

func TestGoWebSocketPackageWithEmptyMessagesCompiles(t *testing.T) {
	buildGoSchema(t, `namespace check;

model PingRequest {}

model StatsRequest {}

model Stats {
  active: int32;
}

union FramePayload {
  @Onkt.tag("ping") ping: PingRequest,
  @Onkt.tag("stats") stats: Stats,
}

model Frame {
  payload: FramePayload;
}

interface Live {
  @Onkt.ws
  @route("/live")
  stream(...Frame): Frame;

  @Onkt.ws
  @route("/ping")
  ping(...PingRequest): PingRequest;

  @get
  @route("/stats")
  stats(...StatsRequest): Stats;
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
