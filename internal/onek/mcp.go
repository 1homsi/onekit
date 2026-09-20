package onek

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type projectInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project directory relative to the server root; defaults to the root. Choose a specific project in repositories with multiple onekit.toml files."`
}
type symbolInput struct {
	Project string `json:"project,omitempty" jsonschema:"Project directory relative to the server root."`
	Query   string `json:"query,omitempty" jsonschema:"Case-insensitive substring of the qualified symbol name; empty lists all symbols."`
	Path    string `json:"path,omitempty" jsonschema:"Optional .onk file path relative to the selected project."`
}
type positionInput struct {
	Project            string `json:"project,omitempty" jsonschema:"Project directory relative to the server root."`
	Path               string `json:"path" jsonschema:".onk file path relative to the selected project."`
	Line               int    `json:"line" jsonschema:"Zero-based line number."`
	Character          int    `json:"character" jsonschema:"Zero-based UTF-16 column (same as LSP)."`
	IncludeDeclaration bool   `json:"includeDeclaration,omitempty" jsonschema:"Include the declaration when finding references."`
}
type symbolOutput struct {
	Symbols     []*LanguageSymbol `json:"symbols"`
	Diagnostics []Diagnostic      `json:"diagnostics"`
}
type positionOutput struct {
	Symbol      *LanguageSymbol `json:"symbol"`
	Locations   []Location      `json:"locations"`
	Diagnostics []Diagnostic    `json:"diagnostics"`
}

// NewLanguageMCPServer exposes read-only compiler-backed queries over MCP.
func NewLanguageMCPServer(dir, version string) (*mcp.Server, error) {
	root, err := canonicalProjectDir(dir)
	if err != nil {
		return nil, err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "onekit", Version: version}, nil)
	snapshot := func(ctx context.Context, project string) (*LanguageSnapshot, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if project == "" {
			project = "."
		}
		if filepath.IsAbs(project) {
			return nil, errors.New("project must be relative to the server root")
		}
		path, err := canonicalProjectDir(filepath.Join(root, project))
		if err != nil {
			return nil, err
		}
		if path != root && !pathWithin(root, path) {
			return nil, errors.New("project must remain inside the server root")
		}
		return AnalyzeLanguage(path, nil)
	}
	tool := func(name, description string) *mcp.Tool {
		closed := false
		return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closed}}
	}
	mcp.AddTool(server, tool("onekit_project", "Validate a OneKit project using its parser/compiler; inspect source files, packages, imports, symbols, and diagnostics. Reads saved .onk files and honors onekit.toml. Does not generate files."), func(ctx context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, *LanguageSnapshot, error) {
		result, err := snapshot(ctx, in.Project)
		return nil, result, err
	})
	mcp.AddTool(server, tool("onekit_symbols", "Find declarations by qualified name, including messages, enums, services, fields, and methods. Returns signatures, documentation, and source locations."), func(ctx context.Context, _ *mcp.CallToolRequest, in symbolInput) (*mcp.CallToolResult, *symbolOutput, error) {
		s, err := snapshot(ctx, in.Project)
		if err != nil {
			return nil, nil, err
		}
		path := ""
		if in.Path != "" {
			path, err = languagePath(s.Root, in.Path)
			if err != nil {
				return nil, nil, err
			}
		}
		return nil, &symbolOutput{Symbols: s.Search(in.Query, path), Diagnostics: s.Diagnostics}, nil
	})
	for _, name := range []string{"onekit_definition", "onekit_references", "onekit_hover"} {
		description := map[string]string{
			"onekit_definition": "Resolve the declaration of a symbol at a source position using compiled type bindings.",
			"onekit_references": "Find type references to the symbol at a source position, respecting import scopes and duplicate names. Covers field/map/oneof types and RPC request/response/error types.",
			"onekit_hover":      "Explain the symbol at a source position with its declaration signature and documentation.",
		}[name] + " Positions use zero-based lines and UTF-16 columns. On compiler errors, declarations remain available but type references are withheld; inspect diagnostics."
		mcp.AddTool(server, tool(name, description), func(ctx context.Context, _ *mcp.CallToolRequest, in positionInput) (*mcp.CallToolResult, *positionOutput, error) {
			if in.Line < 0 || in.Character < 0 {
				return nil, nil, errors.New("line and character must be nonnegative")
			}
			s, err := snapshot(ctx, in.Project)
			if err != nil {
				return nil, nil, err
			}
			path, err := languagePath(s.Root, in.Path)
			if err != nil {
				return nil, nil, err
			}
			if _, ok := s.texts[path]; !ok {
				return nil, nil, fmt.Errorf("file is not in the selected schema project: %s", in.Path)
			}
			symbol := s.SymbolAt(path, Position{in.Line, in.Character})
			out := &positionOutput{Symbol: symbol, Locations: []Location{}, Diagnostics: s.Diagnostics}
			if name == "onekit_references" {
				out.Locations = s.References(symbol, in.IncludeDeclaration)
			} else if symbol != nil {
				out.Locations = append(out.Locations, symbol.Location)
			}
			return nil, out, nil
		})
	}
	return server, nil
}
