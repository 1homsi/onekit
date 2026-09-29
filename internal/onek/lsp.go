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

// rpcOutcome is the reply of a single request: a result, an error, or neither.
type rpcOutcome struct {
	result any
	err    *rpcError
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
			return nil
		}
		var result any
		var callErr *rpcError
		switch {
		case req.JSONRPC != "2.0" || req.Method == "":
			callErr = &rpcError{-32600, "invalid request"}
		case req.Method == "initialize" && !initialized:
			outcome := &rpcOutcome{}
			server.respond(outcome, func() (any, *rpcError) { return server.initialize(req.Params, dir) })
			result, callErr = outcome.result, outcome.err
			initialized = callErr == nil
		case !initialized:
			callErr = &rpcError{-32002, "server not initialized"}
		case shutdown:
			callErr = &rpcError{-32600, "server has shut down"}
		case req.Method == "shutdown":
			shutdown = true
		case req.Method == "initialized" || req.Method == "$/cancelRequest":
		default:
			outcome := &rpcOutcome{}
			server.respond(outcome, func() (any, *rpcError) { return server.handle(req) })
			result, callErr = outcome.result, outcome.err
		}
		if hasID || (callErr != nil && callErr.Code == -32600) {
			if err := writeRPC(out, req.ID, result, callErr); err != nil {
				return err
			}
		} else if callErr != nil {
			if err := writeLSPMessage(out, logMessage(callErr.Message)); err != nil {
				return err
			}
		}
	}
}

// respond runs one request handler into outcome and turns a panic into an
// InternalError. The editor owns this process for as long as the workspace is
// open, so a bug in a handler has to cost the user one failed request rather
// than the session and whatever they had not saved yet. The panic is reported
// through the client log because a bare "internal error" is otherwise
// undiagnosable. outcome is filled in place because a panicking handler never
// reaches its return statement, so nothing would carry the replacement back.
func (s *languageServer) respond(outcome *rpcOutcome, handle func() (any, *rpcError)) {
	defer func() {
		if failure := recover(); failure != nil {
			*outcome = rpcOutcome{err: &rpcError{-32603, "internal error"}}
			_ = writeLSPMessage(s.out, logMessage(fmt.Sprintf("onekit: handler panic: %v", failure)))
		}
	}()
	outcome.result, outcome.err = handle()
}
func logMessage(message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "window/logMessage", "params": map[string]any{"type": 1, "message": message}}
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
	// Validate the position before dispatch, not after: every position-taking
	// method goes through here, and a negative line or character used to reach
	// decoratorCompletion, which then indexed before the start of the line.
	if p.Position.Line < 0 || p.Position.Character < 0 {
		return nil, &rpcError{-32602, "negative position"}
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
	case "textDocument/definition", "textDocument/references", "textDocument/hover", "textDocument/documentSymbol", "workspace/symbol":
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
	case "message":
		return 23
	case "enum":
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
		"completionProvider": map[string]any{"triggerCharacters": []string{"@"}},
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
	// Positions outside the document, negative ones included, have no decorator
	// to complete: answering with an empty list keeps this handler total no
	// matter which caller reaches it with which position.
	if position.Line < 0 || position.Line >= len(lines) || position.Character < 0 {
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
