package onkcompile

import (
	"strings"
	"testing"
)

func TestGoGeneratedNamesCannotBeDeclaredAsMessages(t *testing.T) {
	cases := []struct {
		name, schema string
		options      CompileOptions
		want         string
	}{
		{"runtime type", "\nmodel ServerOptions {\n  a: string;\n}\n\nmodel R {\n  a: string;\n}\n\ninterface S {\n  @post\n  @route(\"/o\")\n  `op`(...R): R;\n}\n", CompileOptions{}, `message name "ServerOptions" collides with a type the Go generators emit`},
		{"validation errors", "\nmodel ValidationErrors {\n  a: string;\n}\n\nmodel R {\n  a: string;\n}\n\ninterface S {\n  @post\n  @route(\"/o\")\n  `op`(...R): R;\n}\n", CompileOptions{}, `"ValidationErrors" collides`},
		{"service server type", "\nmodel SServer {\n  a: string;\n}\n\nmodel R {\n  a: string;\n}\n\ninterface S {\n  @post\n  @route(\"/o\")\n  `op`(...R): R;\n}\n", CompileOptions{}, `collides with the type generated for service S`},
		{"service client type", "\nenum SClient {\n  A,\n}\n\nmodel R {\n  a: string;\n}\n\ninterface S {\n  @post\n  @route(\"/o\")\n  `op`(...R): R;\n}\n", CompileOptions{}, `enum name "SClient" collides with the type generated for service S`},
		{"not generating Go", "\nmodel ServerOptions {\n  a: string;\n}\n\nmodel R {\n  a: string;\n}\n\ninterface S {\n  @post\n  @route(\"/o\")\n  `op`(...R): R;\n}\n", CompileOptions{Targets: []string{"ts-client"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileWithOptions([]Source{{Path: "a.tsp", AST: parseOrFatal(t, `namespace app;
`+tc.schema+"\n")}}, tc.options)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
