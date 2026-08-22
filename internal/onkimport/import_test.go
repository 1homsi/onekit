package onkimport

import (
	"bytes"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
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
		`package pet_adoption`,
		`base_path: "/v1"`,
		// request messages carry bound parameters
		`message ShowPetByIdRequest {`,
		`petId: string`,
		`limit: int32? @query`,
		// body binding is declared at RPC level; the request field stays plain
		`body: NewPet`,
		// named error components are reused canonically instead of synthesized
		`AddPet(AddPetRequest) -> Pet | ValidationProblem @body("body") @post("/pets")`,
		`ShowPetById(ShowPetByIdRequest) -> Pet | ShowPetByIdError404 @get("/pets/{petId}")`,
		// validators mirror string formats
		`id: string @uuid`,
		// optional fields place ? between type and validators
		`owner_email: string? @email`,
		`homepage: string? @uri`,
		`born_at: timestamp`,
		`lives_left: int64`,
		// enum members preserve wire values via @json
		`enum PetStatusValues {`,
		`AVAILABLE @json("available")`,
		// allOf merged into NewPet
		`status: PetStatusValues?`,
		// error messages declare their status
		`message ShowPetByIdError404 @status(404) {`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("generated .onk missing %q:\n%s", want, src)
		}
	}

	if len(result.Warnings) == 0 {
		t.Fatalf("expected warnings for oneOf composition, got none")
	}

	// Quality gate: emitted source must parse AND compile.
	ast, err := onklang.Parse(src)
	if err != nil {
		t.Fatalf("generated .onk does not parse: %v\n%s", err, src)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: result.Package + ".onk", AST: ast}}); err != nil {
		t.Fatalf("generated .onk does not compile: %v\n%s", err, src)
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

func TestImportFormatterStable(t *testing.T) {
	result, err := Import([]byte(petstoreMini), Options{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	formatted, err := onklang.Format(string(result.Source))
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if !bytes.Equal(formatted, result.Source) {
		t.Fatalf("imported output is not formatter-canonical:\n--- imported ---\n%s\n--- formatted ---\n%s", result.Source, formatted)
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
