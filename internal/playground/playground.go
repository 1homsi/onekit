package playground

import (
	"errors"
	"fmt"

	"github.com/1homsi/onekit/internal/gendart"
	"github.com/1homsi/onekit/internal/gengo"
	"github.com/1homsi/onekit/internal/genopenapi"
	"github.com/1homsi/onekit/internal/genpy"
	"github.com/1homsi/onekit/internal/genrust"
	"github.com/1homsi/onekit/internal/gents"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const MaxSchemaBytes = 256 << 10

type Diagnostic struct {
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

type File struct {
	Target  string `json:"target"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type Output struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Files       []File       `json:"files"`
}

func diagnostic(err error) Diagnostic {
	var parseErr *onklang.Error
	if errors.As(err, &parseErr) {
		return Diagnostic{Line: parseErr.Line, Column: parseErr.Column, Code: "parse_error", Message: parseErr.Message}
	}
	var compileErr *onkcompile.Error
	if errors.As(err, &compileErr) {
		return Diagnostic{Line: compileErr.Line, Column: compileErr.Column, Code: compileErr.Code, Message: compileErr.Msg}
	}
	return Diagnostic{Message: err.Error()}
}

func compile(schema string) (*onkir.File, *Diagnostic) {
	if len(schema) > MaxSchemaBytes {
		return nil, &Diagnostic{Message: fmt.Sprintf("the schema exceeds the %d-byte playground limit", MaxSchemaBytes)}
	}
	ast, err := onklang.Parse(schema)
	if err != nil {
		d := diagnostic(err)
		return nil, &d
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "playground.onk", AST: ast}})
	if err != nil {
		d := diagnostic(err)
		return nil, &d
	}
	return pkg.Files[0], nil
}

func Run(schema string) Output {
	out := Output{Diagnostics: []Diagnostic{}, Files: []File{}}
	file, problem := compile(schema)
	if problem != nil {
		out.Diagnostics = append(out.Diagnostics, *problem)
		return out
	}
	add := func(target, name string, content []byte, err error) {
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: "generate_error", Message: fmt.Sprintf("%s %s: %v", target, name, err)})
			return
		}
		if len(content) == 0 {
			return
		}
		out.Files = append(out.Files, File{Target: target, Name: name, Content: string(content)})
	}
	goTypes, err := gengo.GenerateTypes(file)
	add("go", "types.gen.go", goTypes, err)
	goValidate, err := gengo.GenerateValidation(file)
	add("go", "validate.gen.go", goValidate, err)
	goServer, err := gengo.GenerateServer(file)
	add("go", "server.gen.go", goServer, err)
	goClient, err := gengo.GenerateClient(file)
	add("go", "client.gen.go", goClient, err)

	add("typescript", "types.ts", gents.GenerateTypes(file), nil)
	add("typescript", "client.ts", gents.GenerateClient(file), nil)
	add("typescript", "server.ts", gents.GenerateServer(file), nil)

	add("python", "models.py", genpy.GenerateTypes(file), nil)
	add("python", "client.py", genpy.GenerateClient(file, "models"), nil)

	add("dart", "models.dart", gendart.GenerateTypes(file), nil)
	add("dart", "client.dart", gendart.GenerateClient(file), nil)

	add("rust", "types.rs", genrust.GenerateTypes(file), nil)
	add("rust", "server.rs", genrust.GenerateServer(file), nil)
	add("rust", "client.rs", genrust.GenerateClient(file), nil)

	spec, err := genopenapi.Generate(file, genopenapi.Options{Title: "Playground API", Version: "0.1.0"})
	add("openapi", "openapi.yaml", spec, err)
	return out
}

func Format(schema string) (string, *Diagnostic) {
	if len(schema) > MaxSchemaBytes {
		return "", &Diagnostic{Message: fmt.Sprintf("the schema exceeds the %d-byte playground limit", MaxSchemaBytes)}
	}
	formatted, err := onklang.Format(schema)
	if err != nil {
		d := diagnostic(err)
		return "", &d
	}
	return string(formatted), nil
}
