package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/1homsi/onekit/internal/onek"
	"github.com/1homsi/onekit/internal/onkcompat"
	"github.com/1homsi/onekit/internal/onkimport"
	"github.com/1homsi/onekit/internal/onklang"
)

var version = "dev"

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage:
  onek build [--check] [--dir DIR]
  onek check [--json] [--dir DIR]
  onek generate [--dir DIR]
  onek fmt [--check] [--dir DIR | FILE.onk... | -]
  onek watch [--interval DURATION] [--dir DIR]
  onek mock [--addr ADDR] [--seed N] [--error-rate FLOAT] [--latency DURATION] [--dir DIR]
  onek init [--force] [DIR]
  onek import [--out DIR] [--package NAME] [--service NAME] [--force] OPENAPI-FILE
  onek compat [--json] PREVIOUS-DIR CURRENT-DIR
  onek compat [--json] --against GIT-REF [CURRENT-DIR]
  onek mcp [--dir DIR]
  onek lsp [--dir DIR]
  onek version`)
}

func main() {
	os.Exit(exitCode(run(os.Args[1:]), os.Stdout, os.Stderr))
}

func exitCode(err error, stdout, stderr io.Writer) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var diagnosticErr *jsonDiagnosticsExitError
	if errors.As(err, &diagnosticErr) {
		if encodeErr := json.NewEncoder(stdout).Encode(onek.Diagnostics(diagnosticErr.err)); encodeErr != nil {
			fmt.Fprintln(stderr, "onek:", encodeErr)
			return 1
		}
		return diagnosticErr.ExitCode()
	}
	fmt.Fprintln(stderr, "onek:", err)
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		usage(os.Stdout)
		return nil
	}
	if args[0] == "--version" || args[0] == "-v" {
		return runVersion(args[1:])
	}

	switch args[0] {
	case "mcp", "lsp":
		return runLanguageServer(args[0], args[1:])
	case "version":
		return runVersion(args[1:])
	case "build", "generate", "check":
		return runProjectCommand(args[0], args[1:])
	case "fmt":
		return runFormat(args[1:])
	case "init":
		return runInit(args[1:])
	case "watch":
		return runWatch(args[1:])
	case "mock":
		return runMock(args[1:])
	case "import":
		return runImport(args[1:])
	case "compat":
		return runCompat(args[1:])
	default:
		if strings.HasPrefix(args[0], "-") {
			usage(os.Stderr)
		}
		if suggestion := suggestCommand(args[0]); suggestion != "" {
			return fmt.Errorf("unknown command %q (did you mean %q?)", args[0], suggestion)
		}
		return fmt.Errorf("unknown command %q; run 'onek help' for usage", args[0])
	}
}

func runVersion(args []string) error {
	if len(args) != 0 {
		return errors.New("version does not accept arguments")
	}
	info, ok := debug.ReadBuildInfo()
	_, _ = fmt.Fprintln(os.Stdout, resolveVersion(version, info, ok))
	return nil
}

func resolveVersion(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "dev" || !ok || info == nil {
		return stamped
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return stamped
}

func runProjectCommand(command string, args []string) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", ".", "schema project directory")
	asJSON := fs.Bool("json", false, "emit machine-readable diagnostics")
	verify := false
	if command == "build" {
		fs.BoolVar(&verify, "check", false, "fail if generated output differs from what build would write, without writing")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if positional := fs.Args(); len(positional) > 1 {
		return fmt.Errorf("%s accepts at most one directory", command)
	} else if len(positional) == 1 {
		*dir = positional[0]
	}
	var operationErr error
	switch {
	case command == "check":
		operationErr = onek.Check(*dir)
	case verify:
		operationErr = onek.VerifyGenerated(*dir)
	default:
		operationErr = runBuildWithSummary(*dir, *asJSON)
	}
	if !*asJSON {
		return operationErr
	}
	if operationErr != nil {
		return &jsonDiagnosticsExitError{err: operationErr}
	}
	return json.NewEncoder(os.Stdout).Encode([]onek.Diagnostic{})
}

func runFormat(args []string) error {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	check := fs.Bool("check", false, "check formatting without writing files")
	dir := fs.String("dir", ".", "schema project directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	positional := fs.Args()
	if len(positional) == 1 && positional[0] == "-" {
		return formatStdin(os.Stdin, os.Stdout)
	}
	if len(positional) > 0 && allOnkFiles(positional) {
		return onek.FormatFiles(positional, *check)
	}
	if len(positional) > 1 {
		return errors.New("fmt accepts one directory, one or more .onk files, or - for stdin")
	} else if len(positional) == 1 {
		*dir = positional[0]
	}
	return onek.Format(*dir, *check)
}

func allOnkFiles(paths []string) bool {
	for _, path := range paths {
		if !strings.HasSuffix(path, ".onk") {
			return false
		}
	}
	return true
}

func formatStdin(in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	formatted, err := onklang.Format(string(data))
	if err != nil {
		return err
	}
	_, err = out.Write(formatted)
	return err
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("force", false, "overwrite existing starter files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := "."
	if positional := fs.Args(); len(positional) > 1 {
		return errors.New("init accepts at most one directory")
	} else if len(positional) == 1 {
		dir = positional[0]
	}
	return onek.Init(dir, *force)
}

func runWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	interval := fs.Duration("interval", 500*time.Millisecond, "poll interval")
	dir := fs.String("dir", ".", "schema project directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if positional := fs.Args(); len(positional) > 1 {
		return errors.New("watch accepts at most one directory")
	} else if len(positional) == 1 {
		*dir = positional[0]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return onek.Watch(ctx, *dir, *interval, os.Stdout)
}

func runMock(args []string) error {
	fs := flag.NewFlagSet("mock", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	seed := fs.Int64("seed", 1, "deterministic seed for injected errors and latency")
	errorRate := fs.Float64("error-rate", 0, "probability [0,1] of serving a declared typed error")
	latency := fs.Duration("latency", 0, "inject up to this much random latency per request")
	noCORS := fs.Bool("no-cors", false, "do not send CORS headers")
	dir := fs.String("dir", ".", "schema project directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if positional := fs.Args(); len(positional) > 1 {
		return errors.New("mock accepts at most one directory")
	} else if len(positional) == 1 {
		*dir = positional[0]
	}
	server, err := onek.NewMockServer(*dir, onek.MockOptions{
		Addr:      *addr,
		Seed:      *seed,
		ErrorRate: *errorRate,
		Latency:   *latency,
		NoCORS:    *noCORS,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, *addr, os.Stdout)
}

// runImport converts an OpenAPI 3.x document into .onk source, verifies it
// parses, writes it under --out, and reports conversion warnings.
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDir := fs.String("out", "./imported", "directory for the generated .onk file")
	pkg := fs.String("package", "", "generated package name (default: derived from info.title)")
	service := fs.String("service", "", "generated service name (default: package + Service)")
	force := fs.Bool("force", false, "overwrite an existing .onk file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 {
		return errors.New("import requires exactly one OpenAPI file")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("read %s: %w", fs.Arg(0), err)
	}
	result, err := onkimport.Import(data, onkimport.Options{Package: *pkg, Service: *service})
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(os.Stderr, "onek import:", warning)
	}
	// Never write output that does not parse - a broken import is worse than
	// a failed one.
	if _, err := onklang.Parse(string(result.Source)); err != nil {
		return fmt.Errorf("internal: imported schema does not parse: %w", err)
	}
	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", *outDir, err)
	}
	// Defense in depth: package names derive from slug(info.title), but
	// refuse anything that could escape the output directory.
	if result.Package == "" || strings.ContainsAny(result.Package, "/\\") {
		return fmt.Errorf("invalid generated package name %q", result.Package)
	}
	target := filepath.Join(*outDir, result.Package+".onk")
	//nolint:gosec // Same validated target as the write below.
	if _, err := os.Lstat(target); err == nil && !*force {
		return fmt.Errorf("refusing to overwrite %s; pass --force to replace it", target)
	}
	//nolint:gosec // Package is validated against [a-z0-9_]+ above and out dir is user-chosen.
	if err := os.WriteFile(target, result.Source, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Fprintf(os.Stdout, "wrote %s (%d warnings)\n", target, len(result.Warnings))
	return nil
}

func runCompat(args []string) error {
	fs := flag.NewFlagSet("compat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	against := fs.String("against", "", "git ref to use as the previous schema (for example origin/main)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var findings []onkcompat.Finding
	var err error
	switch {
	case *against != "" && len(fs.Args()) <= 1:
		current := "."
		if len(fs.Args()) == 1 {
			current = fs.Arg(0)
		}
		findings, err = onek.CompatibilityAgainstRef(*against, current)
	case *against == "" && len(fs.Args()) == 2:
		findings, err = onek.Compatibility(fs.Arg(0), fs.Arg(1))
	default:
		return errors.New("compat requires PREVIOUS-DIR and CURRENT-DIR, or --against REF [CURRENT-DIR]")
	}
	if err != nil {
		return err
	}
	if *asJSON {
		if encodeErr := json.NewEncoder(os.Stdout).Encode(findings); encodeErr != nil {
			return encodeErr
		}
		if len(findings) > 0 {
			return compatibilityExitError{}
		}
		return nil
	}
	for _, finding := range findings {
		if _, err := fmt.Fprintln(os.Stdout, finding.Path+": "+finding.Message); err != nil {
			return err
		}
	}
	if len(findings) > 0 {
		return compatibilityExitError{}
	}
	return nil
}

type compatibilityExitError struct{}

func (compatibilityExitError) Error() string { return "breaking compatibility changes found" }

func (compatibilityExitError) ExitCode() int { return 2 }

type jsonDiagnosticsExitError struct {
	err error
}

func (e *jsonDiagnosticsExitError) Error() string { return e.err.Error() }

func (*jsonDiagnosticsExitError) ExitCode() int { return 1 }

func runLanguageServer(command string, args []string) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", "", "project directory (LSP defaults to client workspace; MCP defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%s accepts only --dir DIR", command)
	}
	if command == "lsp" {
		return onek.RunLSP(os.Stdin, os.Stdout, *dir)
	}
	if *dir == "" {
		*dir = "."
	}
	info, ok := debug.ReadBuildInfo()
	server, err := onek.NewLanguageMCPServer(*dir, resolveVersion(version, info, ok))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, &mcp.StdioTransport{})
}

var commandNames = []string{"build", "check", "generate", "fmt", "watch", "mock", "init", "import", "compat", "mcp", "lsp", "version", "help"}

var commandAliases = map[string]string{"format": "fmt", "lint": "check", "validate": "check", "gen": "generate", "serve": "mock", "new": "init"}

func suggestCommand(input string) string {
	if alias, ok := commandAliases[input]; ok {
		return alias
	}
	best, bestScore := "", 3
	for _, name := range commandNames {
		score := commandDistance(input, name)
		if strings.HasPrefix(name, input) && len(input) >= 2 {
			score = 0
		}
		if score < bestScore {
			best, bestScore = name, score
		}
	}
	return best
}

func commandDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func runBuildWithSummary(dir string, quiet bool) error {
	summary, err := onek.BuildWithSummary(dir)
	if err != nil || quiet {
		return err
	}
	if len(summary.Targets) == 0 {
		fmt.Fprintln(os.Stderr, "onek: no [generate.*] targets in onekit.toml; nothing was generated")
		return nil
	}
	fmt.Fprintf(os.Stderr, "onek: wrote %d files for %s\n", summary.Files, strings.Join(summary.Targets, ", "))
	return nil
}
