// Package onktest compiles TypeSpec for onekit's tests. A few long-running
// Node processes (typespec/dist/test/server.js) keep the TypeSpec compiler
// loaded, so a test schema compiles in milliseconds; Parse returns the same
// declarations a project build would hand the compiler.
package onktest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/1homsi/onekit/internal/onkschema"
)

// DiagnosticsError is TypeSpec rejecting a schema; each line reads
// "path:line:col - error code: message", with project-relative paths.
type DiagnosticsError struct {
	Lines []string
}

func (e *DiagnosticsError) Error() string {
	return "TypeSpec diagnostics:\n" + strings.Join(e.Lines, "\n")
}

// RepoRoot is the onekit checkout this package lives in.
func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// Compile compiles a TypeSpec project given as project-relative paths
// (forward slashes) and their source. It compiles from main.tsp when the
// project has one, else from every file. The document's paths are
// project-relative too.
func Compile(files map[string]string) (*onkschema.Document, error) {
	withPrelude := make(map[string]string, len(files))
	for name, src := range files {
		withPrelude[name] = addPrelude(src)
	}
	worker, err := pool.get()
	if err != nil {
		return nil, err
	}
	defer pool.put(worker)
	doc, err := worker.compile(withPrelude)
	audit(withPrelude, err)
	return doc, err
}

var auditMu sync.Mutex

// audit appends every rejected schema to $ONKT_TEST_AUDIT, so a review can
// find tests that pass only because their schema never compiled.
func audit(files map[string]string, err error) {
	path := os.Getenv("ONKT_TEST_AUDIT")
	var diagnostics *DiagnosticsError
	if path == "" || !errors.As(err, &diagnostics) {
		return
	}
	auditMu.Lock()
	defer auditMu.Unlock()
	f, openErr := os.OpenFile(path, //nolint:gosec // whoever runs the tests names the audit file
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if openErr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "=== %s\n", strings.Join(diagnostics.Lines, "\n    "))
	for name, src := range files {
		fmt.Fprintf(f, "--- %s\n%s\n", name, src)
	}
}

// Prelude is what every onekit schema starts with. Test schemas may leave
// it out: Compile adds it to a file that imports nothing, on the first line
// so that line numbers stay as written.
const Prelude = `import "@typespec/http"; import "@onkt/typespec"; using TypeSpec.Http; `

var importLine = regexp.MustCompile(`(?m)^\s*import\s`)

func addPrelude(src string) string {
	if importLine.MatchString(src) {
		return src
	}
	return Prelude + src
}

// Parse compiles one TypeSpec file and returns its declarations, the way the
// schema documents the emitter writes describe it.
func Parse(src string) (*onkschema.File, error) {
	doc, err := Compile(map[string]string{"schema.tsp": src})
	if err != nil {
		return nil, err
	}
	switch len(doc.Files) {
	case 0:
		return &onkschema.File{}, nil
	case 1:
		return &doc.Files[0].File, nil
	}
	return nil, fmt.Errorf("schema.tsp declares %d namespaces; use Compile for several", len(doc.Files))
}

// MustParse is Parse that fails the test on an error.
func MustParse(t testing.TB, src string) *onkschema.File {
	t.Helper()
	file, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

// --------------------------------------------------------------- workers

type worker struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	nextID int
}

type response struct {
	ID          int             `json:"id"`
	Document    json.RawMessage `json:"document"`
	Diagnostics []string        `json:"diagnostics"`
	Error       string          `json:"error"`
}

func (w *worker) compile(files map[string]string) (*onkschema.Document, error) {
	w.nextID++
	request, err := json.Marshal(map[string]any{"id": w.nextID, "files": files})
	if err != nil {
		return nil, err
	}
	if _, err := w.in.Write(append(request, '\n')); err != nil {
		return nil, fmt.Errorf("send to the TypeSpec server: %w", err)
	}
	line, err := w.out.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read from the TypeSpec server: %w", err)
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("decode the TypeSpec server's answer: %w", err)
	}
	switch {
	case resp.Error != "":
		return nil, errors.New("TypeSpec server: " + resp.Error)
	case len(resp.Diagnostics) > 0:
		return nil, &DiagnosticsError{Lines: resp.Diagnostics}
	}
	return onkschema.DecodeBytes(resp.Document)
}

var pool = &workerPool{}

type workerPool struct {
	once    sync.Once
	idle    chan *worker
	started int
	limit   int
	mu      sync.Mutex
	err     error
}

func (p *workerPool) get() (*worker, error) {
	p.once.Do(func() {
		p.limit = max(1, runtime.GOMAXPROCS(0)/2)
		p.idle = make(chan *worker, p.limit)
	})
	select {
	case w := <-p.idle:
		return w, nil
	default:
	}
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		return nil, p.err
	}
	if p.started < p.limit {
		p.started++
		p.mu.Unlock()
		w, err := startWorker()
		if err != nil {
			p.mu.Lock()
			p.err = err
			p.mu.Unlock()
			return nil, err
		}
		return w, nil
	}
	p.mu.Unlock()
	return <-p.idle, nil
}

func (p *workerPool) put(w *worker) {
	p.idle <- w
}

func startWorker() (*worker, error) {
	script := filepath.Join(RepoRoot(), "typespec", "dist", "test", "server.js")
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("the TypeSpec test server is not built (%s); run `make typespec`", script)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, errors.New("node is not on PATH; onekit's tests compile TypeSpec with Node.js")
	}
	cmd := exec.CommandContext(context.Background(), node, script)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the TypeSpec server: %w", err)
	}
	return &worker{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20)}, nil
}

// Files compiles several TypeSpec files together, as one project, and
// returns their declarations in path order.
func Files(t testing.TB, files map[string]string) []onkschema.SourceFile {
	t.Helper()
	doc, err := Compile(files)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Files
}
