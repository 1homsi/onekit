package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/1homsi/onekit/examples/simple-api/api"
)

type server struct {
	mu    sync.Mutex
	users map[string]*api.User
	next  int
}

func newServer() *server {
	return &server{users: map[string]*api.User{}}
}

func (s *server) CreateUser(_ context.Context, req *api.CreateUserRequest) (*api.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	u := &api.User{
		Id:        fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", s.next),
		Name:      req.Name,
		Email:     req.Email,
		CreatedAt: time.Now().Unix(),
	}
	s.users[u.Id] = u
	return u, nil
}

func (s *server) GetUser(_ context.Context, req *api.GetUserRequest) (*api.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[req.Id]
	if !ok {
		return nil, errors.New("user not found: " + req.Id)
	}
	return u, nil
}

func (s *server) Login(_ context.Context, req *api.LoginRequest) (*api.LoginResponse, error) {
	if req.AuthMethod == nil {
		return nil, errors.New("auth_method is required")
	}
	return &api.LoginResponse{
		AccessToken:  "demo-access-token",
		RefreshToken: "demo-refresh-token",
		ExpiresIn:    3600,
	}, nil
}

func main() {
	mux := http.NewServeMux()
	api.RegisterUserServiceServer(mux, newServer())
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
