package onek

import (
	"net/http"
	"testing"
)

func TestMockServerAnswersCORSPreflight(t *testing.T) {
	ts := newMockTestServer(t, MockOptions{Seed: 1})
	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/v1/things/abc", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "x-api-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" || resp.Header.Get("Access-Control-Allow-Headers") != "x-api-key" {
		t.Fatalf("preflight: %d %v", resp.StatusCode, resp.Header)
	}
	get, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/things/abc", nil)
	get.Header.Set("Origin", "http://localhost:5173")
	resp, err = http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("GET: %d %v", resp.StatusCode, resp.Header)
	}
}
