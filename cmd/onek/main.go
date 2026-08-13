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
	"strings"
	"syscall"
	"time"

	"github.com/1homsi/onekit/internal/onek"
)

var version = "dev"

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage:
  onek build [--dir DIR]
  onek check [--json] [--dir DIR]
  onek generate [--dir DIR]
  onek fmt [--check] [--dir DIR]
  onek watch [--interval DURATION] [--dir DIR]
  onek init [--force] [DIR]
  onek compat [--json] PREVIOUS-DIR CURRENT-DIR
  onek version`)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var diagnosticErr *jsonDiagnosticsExitError
		if errors.As(err, &diagnosticErr) {
			if encodeErr := json.NewEncoder(os.Stdout).Encode(onek.Diagnostics(diagnosticErr.err)); encodeErr != nil {
				fmt.Fprintln(os.Stderr, "onek:", encodeErr)
				os.Exit(1)
			}
			os.Exit(diagnosticErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "onek:", err)
		code := 1
		var coded interface{ ExitCode() int }
		if errors.As(err, &coded) {
			code = coded.ExitCode()
		}
		os.Exit(code)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		usage(os.Stdout)
		return nil
	}

	switch args[0] {
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
	case "compat":
		return runCompat(args[1:])
	default:
		if strings.HasPrefix(args[0], "-") {
			usage(os.Stderr)
		}
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runVersion(args []string) error {
	if len(args) != 0 {
		return errors.New("version does not accept arguments")
	}
	_, _ = fmt.Fprintln(os.Stdout, version)
	return nil
}

func runProjectCommand(command string, args []string) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", ".", "schema project directory")
	asJSON := fs.Bool("json", false, "emit machine-readable diagnostics")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if positional := fs.Args(); len(positional) > 1 {
		return fmt.Errorf("%s accepts at most one directory", command)
	} else if len(positional) == 1 {
		*dir = positional[0]
	}
	var operationErr error
	if command == "check" {
		operationErr = onek.Check(*dir)
	} else {
		operationErr = onek.Build(*dir)
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
	if positional := fs.Args(); len(positional) > 1 {
		return errors.New("fmt accepts at most one directory")
	} else if len(positional) == 1 {
		*dir = positional[0]
	}
	return onek.Format(*dir, *check)
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

func runCompat(args []string) error {
	fs := flag.NewFlagSet("compat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 2 {
		return errors.New("compat requires PREVIOUS-DIR and CURRENT-DIR")
	}
	findings, err := onek.Compatibility(fs.Arg(0), fs.Arg(1))
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
