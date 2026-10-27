package onkschema

import (
	"strings"
	"testing"
)

func TestDecodeReadsDeclarations(t *testing.T) {
	doc, err := DecodeBytes([]byte(`{
		"version": 1,
		"files": [{
			"path": "/p/main.tsp",
			"package": "app",
			"messages": [{
				"name": "User",
				"line": 3, "col": 7,
				"fields": [
					{"name": "id", "type": {"span": {}, "name": "string"}, "decorators": [{"name": "uuid"}]},
					{"name": "tags", "repeated": true, "type": {"span": {}, "name": "string"}},
					{"name": "labels", "type": {"span": {}, "map": true, "mapKey": "string", "mapValue": {"span": {}, "name": "int32"}}},
					{"name": "auth", "oneof": {"args": [{"name": "discriminator", "value": "kind", "quoted": true}], "variants": [{"name": "email", "type": {"span": {}, "name": "app.Email"}}]}}
				]
			}],
			"services": [{
				"name": "Users",
				"basePath": "/v1",
				"rpcs": [{"name": "get", "request": "app.User", "response": "app.User", "requestSpan": {}, "responseSpan": {}, "decorators": [{"name": "get", "args": [{"value": "/users/{id}", "quoted": true}]}]}]
			}]
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	file := doc.Files[0]
	if file.Path != "/p/main.tsp" || file.Package != "app" {
		t.Fatalf("file = %q %q", file.Path, file.Package)
	}
	user := file.Messages[0]
	if user.Name != "User" || user.Line != 3 || user.Col != 7 || len(user.Fields) != 4 {
		t.Fatalf("message = %+v", user)
	}
	if !user.Fields[1].Repeated || !user.Fields[2].Type.IsMap || user.Fields[2].Type.MapVal.Name != "int32" {
		t.Fatalf("fields = %+v", user.Fields)
	}
	if user.Fields[3].Oneof.Args[0].Value != "kind" || user.Fields[3].Oneof.Variants[0].Type.Name != "app.Email" {
		t.Fatalf("oneof = %+v", user.Fields[3].Oneof)
	}
	rpc := file.Services[0].RPCs[0]
	if rpc.RequestType != "app.User" || rpc.Decorators[0].Args[0].Value != "/users/{id}" {
		t.Fatalf("rpc = %+v", rpc)
	}
}

func TestDecodeRejectsMalformedDocuments(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"version":       {`{"version": 2, "files": []}`, "version 2 is not supported"},
		"unknown key":   {`{"version": 1, "files": [], "extra": true}`, "unknown field"},
		"trailing data": {`{"version": 1, "files": []} {}`, "unexpected data"},
		"no path":       {`{"version": 1, "files": [{"package": "a"}]}`, "has no path"},
		"duplicate":     {`{"version": 1, "files": [{"path": "a", "package": ""}, {"path": "a", "package": ""}]}`, "more than once"},
		"missing type": {
			`{"version": 1, "files": [{"path": "a", "package": "", "messages": [{"name": "M", "fields": [{"name": "f"}]}]}]}`,
			"missing type",
		},
		"map without value": {
			`{"version": 1, "files": [{"path": "a", "package": "", "messages": [{"name": "M", "fields": [{"name": "f", "type": {"span": {}, "map": true, "mapKey": "string"}}]}]}]}`,
			"missing type",
		},
		"type and oneof": {
			`{"version": 1, "files": [{"path": "a", "package": "", "messages": [{"name": "M", "fields": [{"name": "f", "type": {"span": {}, "name": "string"}, "oneof": {"variants": []}}]}]}]}`,
			"both a type and a oneof",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeBytes([]byte(tc.doc))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
