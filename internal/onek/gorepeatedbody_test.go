package onek

import "testing"

func TestGoRepeatedInt64BodyRoundTrips(t *testing.T) {
	buildGoSchema(t, `
package check

message BulkRequest { ids: int64[] }
message BulkResponse { total: int64 }

service Bulk { remove(BulkRequest) -> BulkResponse @post("/bulk") @body("ids") }
`, `package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type bulk struct{}

func (bulk) Remove(ctx context.Context, req *BulkRequest) (*BulkResponse, error) {
	var total int64
	for _, id := range req.Ids {
		total += id
	}
	return &BulkResponse{Total: total}, nil
}

func TestRepeatedBody(t *testing.T) {
	mux := http.NewServeMux()
	if err := RegisterBulkServer(mux, bulk{}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := NewBulkClient(srv.URL).Remove(context.Background(), &BulkRequest{Ids: []int64{1, 2, 9007199254740993}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 9007199254740996 {
		t.Fatalf("unexpected total %d", resp.Total)
	}
}
`)
}
