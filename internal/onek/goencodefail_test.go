package onek

import "testing"

func TestGoServerReturns500WhenResponseCannotEncode(t *testing.T) {
	buildGoSchema(t, `namespace check;

model ScoreRequest {
  id: string;
}

model Score {
  value: float64;
}

interface Scores {
  @get
  @route("/scores/{id}")
  get(...ScoreRequest): Score;
}
`, `package api

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

type scores struct{}

func (scores) Get(ctx context.Context, req *ScoreRequest) (*Score, error) {
	return &Score{Value: math.NaN()}, nil
}

func TestEncodeFailure(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterScoresServer(mux, scores{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/scores/1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("want 500 for an unencodable response, got %d", resp.StatusCode)
	}
}
`)
}
