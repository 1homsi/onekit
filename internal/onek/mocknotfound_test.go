package onek

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMockUnknownRoutesAnswerJSON(t *testing.T) {
	ts := newMockTestServer(t, MockOptions{Seed: 1})
	resp, err := http.Get(ts.URL + "/v1/nope")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || body["message"] != "no mock route for GET /v1/nope" {
		t.Fatalf("unknown route: %d %v", resp.StatusCode, body)
	}
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/things/abc", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET" {
		t.Fatalf("wrong method: %d %q", resp.StatusCode, resp.Header.Get("Allow"))
	}
}
