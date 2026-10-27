package onek

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMockOneofFixturesMatchTheWireShape(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "api.tsp"), `
model Req {
  id: string;
}

model EmailAuth {
  @format("email")
  email: string;
}

@Onkt.discriminator("kind")
union ResNested {
  email: EmailAuth,
  code: string,
}

@discriminated(#{ discriminatorPropertyName: "type", envelope: "none" })
union ResFlat {
  mail: EmailAuth,
}

model Res {
  nested: ResNested;
  flat: ResFlat;
}

interface Svc {
  @get
  @route("/r/{id}")
  get(...Req): Res;
}
`)
	server, err := NewMockServer(dir, MockOptions{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/r/1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	nested, _ := body["nested"]["email"].(map[string]any)
	if body["nested"]["kind"] != "email" || nested["email"] != mockEmail {
		t.Fatalf("nested oneof shape: %v", body["nested"])
	}
	if body["flat"]["type"] != "mail" || body["flat"]["email"] != mockEmail {
		t.Fatalf("flattened oneof shape: %v", body["flat"])
	}
}
