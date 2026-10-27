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

const todoSchema = `import "@typespec/http";
import "@onkt/typespec";

using TypeSpec.Http;

namespace example.todos;

enum Status {
  OPEN,
  DONE,
}

model Todo {
  id: string;
  @Onkt.required @minLength(1) @maxLength(200) title: string;
  status: Status;
  created_at: utcDateTime;
}

model ListTodosRequest {
  @query status?: string;
  @query @minValue(1) @maxValue(100) limit?: int32;
}

model ListTodosResponse {
  todos: Todo[];
}

model CreateTodoRequest {
  @Onkt.required @minLength(1) @maxLength(200) title: string;
}

model GetTodoRequest {
  @Onkt.required id: string;
}

@error
model NotFound {
  @statusCode _: 404;
  message: string;
}

@route("/v1")
interface TodoService {
  @get @route("/todos") listTodos(...ListTodosRequest): ListTodosResponse;

  @post @route("/todos") createTodo(...CreateTodoRequest): Todo;

  @get @route("/todos/{id}") getTodo(...GetTodoRequest): Todo | NotFound;

  /** Streams every todo as server-sent events. */
  @get @route("/watch/todos") @Onkt.stream watchTodos(...ListTodosRequest): Todo;
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
