package onkimport

import (
	"strings"
	"testing"
)

func TestImportKeepsEveryStatusOfASharedErrorSchema(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Pets","version":"1"},"components":{"schemas":{"Error":{"type":"object","properties":{"message":{"type":"string"}}}}},
"paths":{"/pets":{"get":{"operationId":"listPets","responses":{
"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}},
"400":{"description":"bad","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Error"}}}},
"404":{"description":"missing","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Error"}}}},
"5XX":{"description":"server","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Error"}}}}}}}}}`
	result, err := Import([]byte(spec), Options{})
	if err != nil {
		t.Fatal(err)
	}
	src := string(result.Source)
	for _, want := range []string{"@error\nmodel Error {\n  @statusCode _: 400;", "@error\nmodel Error404 {\n  @statusCode _: 404;", "@error\nmodel Error500 {\n  @statusCode _: 500;", "| Error | Error404 | Error500;"} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing %q:\n%s", want, src)
		}
	}
	requireFaithfulTypeSpec(t, result)
}
