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
	"slices"
	"strings"
	"syscall"
	"time"

	"path/filepath"

	"github.com/1homsi/onekit/internal/onek"
	"github.com/1homsi/onekit/internal/onkcompat"
	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkimport"
	"github.com/1homsi/onekit/internal/onklang"
	"github.com/1homsi/onekit/internal/onkmigrate"
)

var version = "dev"

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage:
  onkt build [--check] [--dir DIR]
  onkt check [--json | --format text|json|github] [--dir DIR]
  onkt generate [--dir DIR]
  onkt watch [--interval DURATION] [--dir DIR]
  onkt mock [--addr ADDR] [--seed N] [--error-rate FLOAT] [--latency DURATION] [--watch] [--dir DIR]
  onkt init [--force] [--template NAME | --list-templates] [DIR]
  onkt import [--out DIR] [--package NAME] [--service NAME] [--force] [--stdout] OPENAPI-FILE
  onkt compat [--json] PREVIOUS-DIR CURRENT-DIR
  onkt compat [--json] --against GIT-REF [CURRENT-DIR]
  onkt migrate [--keep] [--force] [--dir DIR]
  onkt version`)
}

func main() {
	warnLegacyName(os.Args[0], os.Stderr)
	os.Exit(exitCode(run(os.Args[1:]), os.Stdout, os.Stderr))
}

// warnLegacyName tells someone running the CLI by its old name, which
// releases still ship so scripts and pinned versions keep working, to switch.
func warnLegacyName(arg0 string, stderr io.Writer) {
	name := arg0[strings.LastIndexAny(arg0, `/\`)+1:]
	if strings.TrimSuffix(name, ".exe") == "onek" {
		fmt.Fprintln(stderr, "onek is now onkt; run onkt instead (the onek name will be removed in a later release)")
	}
}

func exitCode(err error, stdout, stderr io.Writer) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var diagnosticErr *jsonDiagnosticsExitError
	if errors.As(err, &diagnosticErr) {
		if encodeErr := json.NewEncoder(stdout).Encode(onek.Diagnostics(diagnosticErr.err)); encodeErr != nil {
			fmt.Fprintln(stderr, "onkt:", encodeErr)
			return 1
		}
		return diagnosticErr.ExitCode()
	}
	fmt.Fprintln(stderr, "onkt:", err)
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

	pinned, err := pinnedBinary(args[0], args[1:])
	if err != nil {
		return err
	}
	if pinned != "" {
		return reexecPinned(pinned, args)
	}
	switch args[0] {
	case "mcp", "lsp":
		return fmt.Errorf("onkt %s was removed with the .onk language; TypeSpec ships its own language server (the TypeSpec extension for VS Code and other editors)", args[0])
	case "version":
		return runVersion(args[1:])
	case commandBuild, "generate", commandCheck:
		return runProjectCommand(args[0], args[1:])
	case "fmt":
		return errors.New("onkt fmt was removed with the .onk language; format TypeSpec with `npx tsp format \"**/*.tsp\"`")
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
	case "migrate":
		return runMigrate(args[1:])
	default:
		if strings.HasPrefix(args[0], "-") {
			usage(os.Stderr)
		}
		if suggestion := suggestCommand(args[0]); suggestion != "" {
			return fmt.Errorf("unknown command %q (did you mean %q?)", args[0], suggestion)
		}
		return fmt.Errorf("unknown command %q; run 'onkt help' for usage", args[0])
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
	if command == commandBuild {
		fs.BoolVar(&verify, "check", false, "fail if generated output differs from what build would write, without writing")
	}
	format := fs.String("format", "text", "diagnostic format: text, json or github")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *format {
	case "text", "github":
	case "json":
		*asJSON = true
	default:
		return fmt.Errorf("unknown --format %q; use text, json or github", *format)
	}
	if positional := fs.Args(); len(positional) > 1 {
		return fmt.Errorf("%s accepts at most one directory", command)
	} else if len(positional) == 1 {
		*dir = positional[0]
	}
	var operationErr error
	switch {
	case command == commandCheck:
		operationErr = onek.Check(*dir)
	case verify:
		operationErr = onek.VerifyGenerated(*dir)
	default:
		operationErr = runBuildWithSummary(*dir, *asJSON)
	}
	if *format == "github" && operationErr != nil {
		writeGitHubAnnotations(os.Stdout, onek.Diagnostics(operationErr))
		return operationErr
	}
	if !*asJSON {
		return operationErr
	}
	if operationErr != nil {
		return &jsonDiagnosticsExitError{err: operationErr}
	}
	return json.NewEncoder(os.Stdout).Encode([]onek.Diagnostic{})
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("force", false, "overwrite existing starter files")
	template := fs.String("template", "go", "starter project to create (see --list-templates)")
	list := fs.Bool("list-templates", false, "print the available starter projects and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *list {
		for _, t := range onek.InitTemplates() {
			fmt.Fprintf(os.Stdout, "%-10s %s\n", t.Name, t.Description)
		}
		return nil
	}
	dir := "."
	if positional := fs.Args(); len(positional) > 1 {
		return errors.New("init accepts at most one directory")
	} else if len(positional) == 1 {
		dir = positional[0]
	}
	if err := onek.InitTemplateProject(dir, *force, *template); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "onkt: created onkt.toml, main.tsp and package.json (%s template); run `npm install`, edit main.tsp, then run: onkt build --dir %s\n", *template, dir)
	return nil
}

func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", ".", "schema project directory")
	keep := fs.Bool("keep", false, "keep the .onk files after writing the .tsp files")
	force := fs.Bool("force", false, "overwrite existing .tsp files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("migrate does not accept positional arguments; use --dir")
	}
	result, err := onek.Migrate(*dir, onek.MigrateOptions{KeepSchemas: *keep, Force: *force})
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "onkt:", onek.DescribeMigration(result))
	return nil
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
	watch := fs.Bool("watch", false, "reload routes and fixtures when the schema changes")
	dir := fs.String("dir", ".", "schema project directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if positional := fs.Args(); len(positional) > 1 {
		return errors.New("mock accepts at most one directory")
	} else if len(positional) == 1 {
		*dir = positional[0]
	}
	opts := onek.MockOptions{
		Addr:      *addr,
		Seed:      *seed,
		ErrorRate: *errorRate,
		Latency:   *latency,
		NoCORS:    *noCORS,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *watch {
		return onek.RunMockWithReload(ctx, *dir, opts, 500*time.Millisecond, os.Stdout)
	}
	server, err := onek.NewMockServer(*dir, opts)
	if err != nil {
		return err
	}
	return server.Run(ctx, *addr, os.Stdout)
}

// runImport converts an OpenAPI 3.x document into .onk source, verifies it
// parses, writes it under --out, and reports conversion warnings.
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDir := fs.String("out", "./imported", "directory for the generated TypeSpec files")
	pkg := fs.String("package", "", "generated namespace (default: derived from info.title)")
	service := fs.String("service", "", "generated service name (default: package + Service)")
	force := fs.Bool("force", false, "overwrite an existing .tsp file")
	toStdout := fs.Bool("stdout", false, "print the schema instead of writing it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 {
		return errors.New("import requires exactly one OpenAPI (.yaml, .json) file")
	}
	if strings.EqualFold(filepath.Ext(fs.Arg(0)), ".proto") {
		return errors.New("onkt no longer imports Protocol Buffers; import an OpenAPI document instead")
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
		fmt.Fprintln(os.Stderr, "onkt import:", warning)
	}
	// Defense in depth: package names derive from slug(info.title), but
	// refuse anything that could escape the output directory.
	if result.Package == "" || strings.ContainsAny(result.Package, "/\\") {
		return fmt.Errorf("invalid generated package name %q", result.Package)
	}
	target := filepath.Join(*outDir, result.Package+".tsp")
	schema, err := importedTypeSpec(result, target)
	if err != nil {
		return err
	}
	if *toStdout {
		_, err := io.WriteString(os.Stdout, schema)
		return err
	}
	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", *outDir, err)
	}
	//nolint:gosec // Same validated target as the write below.
	if _, err := os.Lstat(target); err == nil && !*force {
		return fmt.Errorf("refusing to overwrite %s; pass --force to replace it", target)
	}
	//nolint:gosec // Package is validated against [a-z0-9_]+ above and out dir is user-chosen.
	if err := os.WriteFile(target, []byte(schema), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	entry := filepath.Join(*outDir, onkmigrate.EntryFile)
	if _, err := os.Lstat(entry); errors.Is(err, os.ErrNotExist) {
		main := fmt.Sprintf("import \"./%s\";\n", filepath.Base(target))
		//nolint:gosec // main.tsp inside the user-chosen out dir.
		if err := os.WriteFile(entry, []byte(main), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", entry, err)
		}
	}
	fmt.Fprintf(os.Stdout, "wrote %s (%d warnings)\n", target, len(result.Warnings))
	return nil
}

// importedTypeSpec checks an imported schema and converts it to TypeSpec.
// Never write output that does not compile - a broken import is worse than a
// failed one.
func importedTypeSpec(result *onkimport.Result, target string) (string, error) {
	ast, err := onklang.Parse(string(result.Source))
	if err != nil {
		return "", fmt.Errorf("internal: imported schema does not parse: %w", err)
	}
	if _, err := onkcompile.Compile([]onkcompile.Source{{Path: result.Package + ".onk", AST: ast}}); err != nil {
		return "", fmt.Errorf("imported schema does not pass onkt check: %w", err)
	}
	out, err := onkmigrate.Convert([]onkmigrate.Source{{Path: target, AST: ast}}, onkmigrate.Options{})
	if err != nil {
		return "", fmt.Errorf("internal: imported schema does not convert to TypeSpec: %w", err)
	}
	return out[0].Content, nil
}

func runCompat(args []string) error {
	fs := flag.NewFlagSet("compat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	against := fs.String("against", "", "git ref to use as the previous schema (for example origin/main)")
	var allowed []string
	fs.Func("allow", "accept findings for this path or anything under it (repeatable)", func(value string) error {
		allowed = append(allowed, value)
		return nil
	})
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
	findings = withoutAllowed(findings, allowed)
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
		line := finding.Path + ": " + finding.Message
		if finding.Before != "" || finding.After != "" {
			line += fmt.Sprintf(" (%q -> %q)", finding.Before, finding.After)
		}
		if _, err := fmt.Fprintln(os.Stdout, line); err != nil {
			return err
		}
	}
	if len(findings) > 0 {
		return compatibilityExitError{}
	}
	return nil
}

func withoutAllowed(findings []onkcompat.Finding, allowed []string) []onkcompat.Finding {
	if len(allowed) == 0 {
		return findings
	}
	kept := findings[:0]
	for _, finding := range findings {
		if !slices.ContainsFunc(allowed, func(path string) bool {
			return finding.Path == path || strings.HasPrefix(finding.Path, path+".") || strings.HasPrefix(finding.Path, path+" ")
		}) {
			kept = append(kept, finding)
		}
	}
	return kept
}

type compatibilityExitError struct{}

func (compatibilityExitError) Error() string { return "breaking compatibility changes found" }

func (compatibilityExitError) ExitCode() int { return 2 }

type jsonDiagnosticsExitError struct {
	err error
}

func (e *jsonDiagnosticsExitError) Error() string { return e.err.Error() }

func (*jsonDiagnosticsExitError) ExitCode() int { return 1 }

var commandNames = []string{"build", "check", "generate", "watch", "mock", "init", "import", "compat", "migrate", "version", "help"}

var commandAliases = map[string]string{"lint": "check", "validate": "check", "gen": "generate", "serve": "mock", "new": "init"}

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
		fmt.Fprintln(os.Stderr, "onkt: no [generate.*] targets in onkt.toml; nothing was generated")
		return nil
	}
	if summary.Cached {
		fmt.Fprintf(os.Stderr, "onkt: up to date: %d files for %s\n", summary.Files, strings.Join(summary.Targets, ", "))
		return nil
	}
	fmt.Fprintf(os.Stderr, "onkt: wrote %d files for %s\n", summary.Files, strings.Join(summary.Targets, ", "))
	return nil
}

func writeGitHubAnnotations(w io.Writer, diagnostics []onek.Diagnostic) {
	cwd, _ := os.Getwd()
	escape := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	property := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	for _, d := range diagnostics {
		var props []string
		if d.Path != "" {
			path := d.Path
			if rel, err := filepath.Rel(cwd, path); err == nil && filepath.IsAbs(path) && !strings.HasPrefix(rel, "..") {
				path = rel
			}
			props = append(props, "file="+property.Replace(filepath.ToSlash(path)))
		}
		if d.Line > 0 {
			props = append(props, fmt.Sprintf("line=%d", d.Line))
		}
		if d.Column > 0 {
			props = append(props, fmt.Sprintf("col=%d", d.Column))
		}
		if d.Code != "" {
			props = append(props, "title="+property.Replace("onkt "+d.Code))
		}
		fmt.Fprintf(w, "::error %s::%s\n", strings.Join(props, ","), escape.Replace(d.Message))
	}
}
