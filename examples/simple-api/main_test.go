package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1homsi/onekit/examples/simple-api/api"
)

func TestUserServiceEndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	api.RegisterUserServiceServer(mux, newServer())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := api.NewUserServiceClient(srv.URL)
	client.Headers["X-API-Key"] = "123e4567-e89b-12d3-a456-426614174000"
	client.Headers["X-Request-Id"] = "223e4567-e89b-12d3-a456-426614174000"
	ctx := context.Background()

	created, err := client.CreateUser(ctx, &api.CreateUserRequest{Name: "Alice Johnson", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.Name != "Alice Johnson" || created.Email != "alice@example.com" || created.Id == "" {
		t.Fatalf("unexpected created user: %+v", created)
	}

	fetched, err := client.GetUser(ctx, &api.GetUserRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if fetched.Id != created.Id {
		t.Fatalf("unexpected fetched user: %+v", fetched)
	}

	if _, err := client.GetUser(ctx, &api.GetUserRequest{Id: "does-not-exist"}); err == nil {
		t.Fatalf("expected error for missing user")
	}

	loginReq := &api.LoginRequest{
		DeviceId: "device-1",
		AuthMethod: &api.LoginRequestAuthMethodEmail{
			Email: &api.EmailAuth{Email: "alice@example.com", Password: "hunter22"},
		},
	}
	loginResp, err := client.Login(ctx, loginReq)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if loginResp.AccessToken == "" {
		t.Fatalf("expected access token, got %+v", loginResp)
	}

	invalidCreate := &api.CreateUserRequest{Name: "A", Email: "not-an-email"}
	if _, err := client.CreateUser(ctx, invalidCreate); err == nil {
		t.Fatalf("expected validation error for invalid create request")
	}
}
