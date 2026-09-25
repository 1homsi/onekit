package onek

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/1homsi/onekit/internal/onklang"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type lspDocument struct {
	URI     string `json:"uri"`
	Text    string `json:"text"`
	Version int    `json:"version"`
}
type lspParams struct {
	TextDocument lspDocument `json:"textDocument"`
	Position     Position    `json:"position"`
	Query        string      `json:"query"`
	NewName      string      `json:"newName"`
	Context      struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
	ContentChanges []struct {
		Text  string `json:"text"`
		Range *Range `json:"range"`
	} `json:"contentChanges"`
}
type languageServer struct {
	root      string
	overlays  map[string]string
	versions  map[string]int
	published map[string]bool
	out       io.Writer
}

// RunLSP serves one workspace over stdio, with full-document synchronization.
// An explicit dir overrides the client's root URI (useful for monorepos).
func RunLSP(in io.Reader, out io.Writer, dir string) error {
	server := &languageServer{overlays: map[string]string{}, versions: map[string]int{}, published: map[string]bool{}, out: out}
	reader := bufio.NewReader(in)
	initialized, shutdown := false, false
	for {
		payload, err := readLSPMessage(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		var req rpcRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			if err := writeRPC(out, nil, nil, &rpcError{-32700, "parse error"}); err != nil {
				return err
			}
			continue
		}
		hasID := len(req.ID) > 0 && string(req.ID) != "null"
		if req.Method == "exit" {
			if !shutdown {
				return errExitBeforeShutdown
			}
			return nil
		}
		var result any
		var callErr *rpcError
		switch {
		case req.JSONRPC != "2.0" || req.Method == "":
			callErr = &rpcError{-32600, "invalid request"}
		case req.Method == "initialize" && !initialized:
			result, callErr = server.initialize(req.Params, dir)
			initialized = callErr == nil
		case !initialized:
			callErr = &rpcError{-32002, "server not initialized"}
		case shutdown:
			callErr = &rpcError{-32600, "server has shut down"}
		case req.Method == "shutdown":
			shutdown = true
		case req.Method == "initialized" || req.Method == "$/cancelRequest":
		default:
			result, callErr = server.handle(req)
		}
		if hasID || (callErr != nil && callErr.Code == -32600) {
			if err := writeRPC(out, req.ID, result, callErr); err != nil {
				return err
			}
		} else if callErr != nil && callErr.Code != -32601 {
			if err := writeLSPMessage(out, map[string]any{"jsonrpc": "2.0", "method": "window/logMessage", "params": map[string]any{"type": 1, "message": callErr.Message}}); err != nil {
				return err
			}
		}
	}
}

var errExitBeforeShutdown = errors.New("language server received exit before shutdown")

func (s *languageServer) formatDocument(path string) (any, *rpcError) {
	text, open := s.overlays[path]
	if !open {
		data, err := readRegularFile(path)
		if err != nil {
			return nil, &rpcError{-32603, err.Error()}
		}
		text = string(data)
	}
	formatted, err := onklang.Format(text)
	if err != nil || string(formatted) == text || len(formatted) == 0 {
		return []any{}, nil
	}
	lines := strings.Split(text, "\n")
	last := lines[len(lines)-1]
	end := map[string]int{"line": len(lines) - 1, "character": len(utf16.Encode([]rune(last)))}
	return []any{map[string]any{
		"range":   map[string]any{"start": map[string]int{"line": 0, "character": 0}, "end": end},
		"newText": string(formatted),
	}}, nil
}

func (s *languageServer) handle(req rpcRequest) (any, *rpcError) {
	var p lspParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params"}
		}
	}
	path := ""
	if strings.HasPrefix(req.Method, "textDocument/") {
		value, err := pathFromURI(p.TextDocument.URI)
		if err == nil {
			path, err = languagePath(s.root, value)
		}
		if err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		if root, err := resolveSchemaTree(s.root); err == nil && !pathWithin(root, path) {
			return nil, &rpcError{-32602, "document is outside the configured schema root"}
		}
	}
	changed := false
	switch req.Method {
	case "textDocument/didOpen":
		if len(p.TextDocument.Text) > maxInputFileBytes {
			return nil, &rpcError{-32602, "document exceeds input limit"}
		}
		if len(s.overlays) >= maxInputFileCount {
			return nil, &rpcError{-32602, "too many open documents"}
		}
		s.overlays[path] = p.TextDocument.Text
		s.versions[path] = p.TextDocument.Version
		changed = true
	case "textDocument/didChange":
		version, open := s.versions[path]
		if !open {
			return nil, &rpcError{-32602, "document is not open"}
		}
		if p.TextDocument.Version <= version {
			return nil, nil
		}
		if len(p.ContentChanges) != 1 || p.ContentChanges[0].Range != nil {
			return nil, &rpcError{-32602, "expected one full-document change"}
		}
		if len(p.ContentChanges[0].Text) > maxInputFileBytes {
			return nil, &rpcError{-32602, "document exceeds input limit"}
		}
		s.overlays[path] = p.ContentChanges[0].Text
		s.versions[path] = p.TextDocument.Version
		changed = true
	case "textDocument/didClose":
		delete(s.overlays, path)
		delete(s.versions, path)
		changed = true
	case "textDocument/didSave", "workspace/didChangeWatchedFiles":
		changed = true
	case "textDocument/completion":
		return s.decoratorCompletion(path, p.Position), nil
	case "textDocument/formatting":
		return s.formatDocument(path)
	case "textDocument/definition", "textDocument/references", "textDocument/hover", "textDocument/documentSymbol", "workspace/symbol",
		"textDocument/prepareRename", "textDocument/rename":
	default:
		return nil, &rpcError{-32601, "method not found"}
	}
	snapshot, err := AnalyzeLanguage(s.root, s.overlays)
	if err != nil {
		return nil, &rpcError{-32603, err.Error()}
	}
	if changed {
		if err := s.publishDiagnostics(snapshot); err != nil {
			return nil, &rpcError{-32603, err.Error()}
		}
		return nil, nil
	}
	if p.Position.Line < 0 || p.Position.Character < 0 {
		return nil, &rpcError{-32602, "negative position"}
	}
	symbol := snapshot.SymbolAt(path, p.Position)
	switch req.Method {
	case "textDocument/definition":
		if symbol != nil {
			return lspLocation(symbol.Location), nil
		}
		return nil, nil
	case "textDocument/references":
		locations := []any{}
		for _, location := range snapshot.References(symbol, p.Context.IncludeDeclaration) {
			locations = append(locations, lspLocation(location))
		}
		return locations, nil
	case "textDocument/prepareRename":
		return prepareRename(snapshot, symbol, path, p.Position), nil
	case "textDocument/rename":
		return renameSymbol(snapshot, symbol, p.NewName)
	case "textDocument/hover":
		if symbol == nil {
			return nil, nil
		}
		text := symbol.Detail
		if symbol.Documentation != "" {
			text += "\n\n" + symbol.Documentation
		}
		return map[string]any{"contents": map[string]string{"kind": "plaintext", "value": text}}, nil
	default:
		symbols := []any{}
		filter := ""
		if req.Method == "textDocument/documentSymbol" {
			filter = path
		}
		for _, symbol := range snapshot.Search(p.Query, filter) {
			symbols = append(symbols, map[string]any{"name": symbol.Name, "kind": lspSymbolKind(symbol.Kind), "containerName": symbol.QualifiedName, "location": lspLocation(symbol.Location)})
		}
		return symbols, nil
	}
}
func (s *languageServer) publishDiagnostics(snapshot *LanguageSnapshot) error {
	grouped := map[string][]any{}
	for path := range s.published {
		grouped[path] = []any{}
	}
	for path := range s.overlays {
		grouped[path] = []any{}
	}
	for _, d := range snapshot.Diagnostics {
		path := d.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.root, path)
		}
		r := snapshot.sourceRange(path, onklang.Span{Line: max(d.Line, 1), Col: max(d.Column, 1), EndLine: max(d.Line, 1), EndCol: max(d.Column, 1) + 1})
		grouped[path] = append(grouped[path], map[string]any{"range": r, "severity": 1, "source": "onek", "code": d.Code, "message": d.Message})
	}
	next := map[string]bool{}
	for path, diagnostics := range grouped {
		if err := writeLSPMessage(s.out, map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": fileURI(path), "diagnostics": diagnostics}}); err != nil {
			return err
		}
		if len(diagnostics) > 0 {
			next[path] = true
		}
	}
	s.published = next
	return nil
}
func lspSymbolKind(kind string) int {
	switch kind {
	case symbolKindMessage:
		return 23
	case symbolKindEnum:
		return 10
	case "enumMember":
		return 22
	case "service":
		return 11
	case "method":
		return 6
	default:
		return 8
	}
}
func lspLocation(l Location) map[string]any {
	return map[string]any{"uri": fileURI(l.Path), "range": l.Range}
}
func fileURI(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
func pathFromURI(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("expected a local file URI")
	}
	path := u.Path
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		return "", errors.New("file URI must contain an absolute path")
	}
	return filepath.Clean(path), nil
}
func readLSPMessage(reader *bufio.Reader) ([]byte, error) {
	length := -1
	size := 0
	for {
		// ReadSlice bounds even a header that never sends a newline.
		line, err := reader.ReadSlice('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && (size > 0 || len(line) > 0) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		size += len(line)
		if size > 8192 {
			return nil, errors.New("LSP headers too large")
		}
		value := strings.TrimSpace(string(line))
		if value == "" {
			break
		}
		key, v, ok := strings.Cut(value, ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			if length != -1 {
				return nil, errors.New("duplicate Content-Length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(v))
			if err != nil || length < 0 || length > maxInputFileBytes*2 {
				return nil, errors.New("invalid Content-Length")
			}
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	payload := make([]byte, length)
	_, err := io.ReadFull(reader, payload)
	return payload, err
}
func writeRPC(out io.Writer, id json.RawMessage, result any, rpcErr *rpcError) error {
	message := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		message["error"] = rpcErr
	} else {
		message["result"] = result
	}
	return writeLSPMessage(out, message)
}
func writeLSPMessage(out io.Writer, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}

func (s *languageServer) initialize(raw json.RawMessage, dir string) (any, *rpcError) {
	var err error
	var params struct {
		RootURI          string `json:"rootUri"`
		WorkspaceFolders []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{-32602, "invalid initialize params"}
	}
	root := dir
	if root == "" {
		uri := params.RootURI
		if uri == "" && len(params.WorkspaceFolders) > 0 {
			uri = params.WorkspaceFolders[0].URI
		}
		if uri != "" {
			root, err = pathFromURI(uri)
		} else {
			root = "."
		}
	}
	if err == nil {
		s.root, err = canonicalProjectDir(root)
	}
	if err != nil {
		return nil, &rpcError{-32602, err.Error()}
	}
	return map[string]any{"serverInfo": map[string]string{"name": "onekit"}, "capabilities": map[string]any{
		"positionEncoding": "utf-16", "textDocumentSync": map[string]any{"openClose": true, "change": 1, "save": true},
		"definitionProvider": true, "referencesProvider": true, "hoverProvider": true, "documentSymbolProvider": true, "workspaceSymbolProvider": true,
		"renameProvider":             map[string]any{"prepareProvider": true},
		"documentFormattingProvider": true,
		"completionProvider":         map[string]any{"triggerCharacters": []string{"@"}},
	}}, nil
}

func (s *languageServer) decoratorCompletion(path string, position Position) []map[string]any {
	text, ok := s.overlays[path]
	if !ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return []map[string]any{}
		}
		text = string(data)
	}
	lines := strings.Split(text, "\n")
	if position.Line < 0 || position.Line >= len(lines) {
		return []map[string]any{}
	}
	line := []rune(lines[position.Line])
	end := min(position.Character, len(line))
	start := end
	for start > 0 && (line[start-1] == '_' || unicode.IsLetter(line[start-1]) || unicode.IsDigit(line[start-1])) {
		start--
	}
	if start == 0 || line[start-1] != '@' {
		return []map[string]any{}
	}
	return DecoratorCompletions(string(line[start:end]))
}
