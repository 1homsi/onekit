package onek

import (
	"fmt"
	"strings"
)

type InitTemplate struct {
	Name        string
	Description string
	config      string
	schema      string
}

const todoSchema = `package example.todos

enum Status {
  OPEN
  DONE
}

message Todo {
  id: string
  title: string @required @len(1, 200)
  status: Status
  created_at: timestamp
}

message ListTodosRequest {
  status: string? @query
  limit: int32? @query @range(1, 100)
}

message ListTodosResponse {
  todos: Todo[]
}

message CreateTodoRequest {
  title: string @required @len(1, 200)
}

message GetTodoRequest {
  id: string @required
}

message NotFound @status(404) {
  message: string
}

service TodoService {
  base_path: "/v1"

  listTodos(ListTodosRequest) -> ListTodosResponse @get("/todos")
  createTodo(CreateTodoRequest) -> Todo @post("/todos")
  getTodo(GetTodoRequest) -> Todo | NotFound @get("/todos/{id}")
  watchTodos(ListTodosRequest) -> Todo @get("/watch/todos") @stream
}
`

const goOpenAPITail = `
[generate.openapi]
out = "./docs"
title = "My API"
version = "0.1.0"
`

var initTemplates = []InitTemplate{
	{
		Name:        "go",
		Description: "Go server and client with an OpenAPI document (the default)",
		config:      initConfig,
		schema:      initSchema,
	},
	{
		Name:        "web",
		Description: "Go server with a dependency-free TypeScript client (plain fetch) and OpenAPI",
		config: `module = "example.com/%s/gen/go"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"

[generate.ts-client]
out = "./web/src/api"
` + goOpenAPITail,
		schema: todoSchema,
	},
	{
		Name:        "fullstack",
		Description: "Go server with TypeScript, Dart/Flutter and Swift clients, and OpenAPI",
		config: `module = "example.com/%s/gen/go"

[generate.go-server]
out = "./gen/go"

[generate.go-client]
out = "./gen/go"

[generate.ts-client]
out = "./web/src/api"

[generate.dart-client]
out = "./mobile/lib/api"

[generate.swift-client]
out = "./ios/Sources/Api"
` + goOpenAPITail,
		schema: todoSchema,
	},
	{
		Name:        "mobile",
		Description: "Dart/Flutter and Swift clients plus OpenAPI, for a backend you already have",
		config: `module = "example.com/%s"

[generate.dart-client]
out = "./mobile/lib/api"

[generate.swift-client]
out = "./ios/Sources/Api"
` + goOpenAPITail,
		schema: todoSchema,
	},
	{
		Name:        "ts",
		Description: "TypeScript server routes and client for Node, Bun, Deno or Workers",
		config: `module = "example.com/%s"

[generate.ts-server]
out = "./server/src/api"

[generate.ts-client]
out = "./web/src/api"
` + goOpenAPITail,
		schema: todoSchema,
	},
	{
		Name:        "rust",
		Description: "Rust (axum) server and client with an OpenAPI document",
		config: `module = "example.com/%s"

[generate.rust-server]
out = "./src/generated"

[generate.rust-client]
out = "./src/generated"
` + goOpenAPITail,
		schema: todoSchema,
	},
}

func InitTemplates() []InitTemplate {
	return append([]InitTemplate(nil), initTemplates...)
}

func findInitTemplate(name string) (InitTemplate, error) {
	if name == "" {
		name = "go"
	}
	names := make([]string, 0, len(initTemplates))
	for _, template := range initTemplates {
		if template.Name == name {
			return template, nil
		}
		names = append(names, template.Name)
	}
	return InitTemplate{}, fmt.Errorf("unknown init template %q (available: %s)", name, strings.Join(names, ", "))
}
