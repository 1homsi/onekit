package onkimport

import (
	"bytes"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompat"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const petstoreMini = `
openapi: 3.0.3
info:
  title: Pet Adoption
  version: 1.0.0
servers:
  - url: https://api.example.com/v1
paths:
  /pets/{petId}:
    get:
      operationId: showPetById
      parameters:
        - name: petId
          in: path
          required: true
          schema: { type: string }
        - name: limit
          in: query
          schema: { type: integer, format: int32 }
      responses:
        "200":
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Pet" }
        "404":
          content:
            application/json:
              schema: { type: object, properties: { message: { type: string } } }
  /pets:
    post:
      operationId: addPet
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/NewPet" }
      parameters:
        - name: dry_run
          in: query
          schema: { type: boolean }
      responses:
        "201":
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Pet" }
        "422":
          content:
            application/json:
              schema: { $ref: "#/components/schemas/ValidationProblem" }
components:
  schemas:
    PetStatus:
      type: string
      enum: [available, pending, sold]
    Category:
      type: object
      required: [name]
      properties:
        name: { type: string }
    NewPet:
      allOf:
        - type: object
          required: [name]
          properties:
            name: { type: string, minLength: 2 }
        - type: object
          properties:
            status: { $ref: "#/components/schemas/PetStatus" }
    Pet:
      type: object
      required: [id, name]
      properties:
        id: { type: string, format: uuid }
        name: { type: string }
        owner_email: { type: string, format: email }
        homepage: { type: string, format: uri }
        born_at: { type: string, format: date-time }
        weight_kg: { type: number }
        lives_left: { type: integer, format: int64 }
        status: { $ref: "#/components/schemas/PetStatus" }
        category: { $ref: "#/components/schemas/Category" }
        nicknames: { type: array, items: { type: string } }
        metadata:
          oneOf:
            - type: string
            - type: object
    ValidationProblem:
      type: object
      properties:
        message: { type: string }
`

func TestImportConvertsPetstoreMini(t *testing.T) {
	result, err := Import([]byte(petstoreMini), Options{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	src := string(result.Source)

	if result.Package != "pet_adoption" {
		t.Fatalf("package = %q, want pet_adoption", result.Package)
	}
	for _, want := range []string{
		"namespace pet_adoption;\n",
		"@route(\"/v1\")\ninterface PetAdoptionService {",
		// request messages carry bound parameters
		"model ShowPetByIdRequest {",
		"  petId: string;\n",
		"  @query\n  limit?: int32;\n",
		// the request body is the request's @body field
		"  @body\n  body: NewPet;\n",
		// named error components are reused canonically instead of synthesized
		"  @post\n  @route(\"/pets\")\n  AddPet(...AddPetRequest): Pet | ValidationProblem;\n",
		"  @get\n  @route(\"/pets/{petId}\")\n  ShowPetById(...ShowPetByIdRequest): Pet | ShowPetByIdError404;\n",
		// validators mirror string formats
		"  @format(\"uuid\")\n  id: string;\n",
		"  @format(\"email\")\n  owner_email?: string;\n",
		"  @format(\"uri\")\n  homepage?: string;\n",
		"  born_at?: utcDateTime;\n",
		"  lives_left?: int64;\n",
		// enum members preserve wire values
		"enum PetStatusValues {",
		"  AVAILABLE: \"available\",\n",
		// allOf merged into NewPet
		"  status?: PetStatusValues;\n",
		// error messages declare their status
		"@error\nmodel ShowPetByIdError404 {\n  @statusCode _: 404;\n",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("imported TypeSpec missing %q:\n%s", want, src)
		}
	}

	if len(result.Warnings) == 0 {
		t.Fatalf("expected warnings for oneOf composition, got none")
	}
	requireFaithfulTypeSpec(t, result)
}

// requireFaithfulTypeSpec holds the quality gate every import passes: the
// TypeSpec compiles, and it means exactly the declarations the importer built.
func requireFaithfulTypeSpec(t *testing.T, result *Result) {
	t.Helper()
	built, err := onkcompile.Compile([]onkcompile.Source{{Path: result.Package + ".tsp", AST: result.File}})
	if err != nil {
		t.Fatalf("imported declarations do not compile: %v", err)
	}
	ast, err := onktest.Parse(string(result.Source))
	if err != nil {
		t.Fatalf("imported TypeSpec does not compile: %v\n%s", err, result.Source)
	}
	spelled, err := onkcompile.Compile([]onkcompile.Source{{Path: result.Package + ".tsp", AST: ast}})
	if err != nil {
		t.Fatalf("imported TypeSpec does not pass onkt check: %v\n%s", err, result.Source)
	}
	if findings := onkcompat.Compare(built, spelled); len(findings) != 0 {
		t.Fatalf("the TypeSpec differs from the imported declarations: %+v\n%s", findings, result.Source)
	}
}

func TestImportIsDeterministic(t *testing.T) {
	first, err := Import([]byte(petstoreMini), Options{Package: "x"})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	second, err := Import([]byte(petstoreMini), Options{Package: "x"})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !bytes.Equal(first.Source, second.Source) {
		t.Fatalf("import is not deterministic")
	}
}

func TestImportRejectsNonOpenAPI(t *testing.T) {
	if _, err := Import([]byte("swagger: \"2.0\""), Options{}); err == nil {
		t.Fatal("expected rejection of non-3.x document")
	}
	if _, err := Import(nil, Options{}); err == nil {
		t.Fatal("expected rejection of empty document")
	}
}
