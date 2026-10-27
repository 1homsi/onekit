package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/1homsi/onekit/internal/onek"
)

const (
	commandBuild = "build"
	commandCheck = "check"
	pinReexecEnv = "ONKT_PINNED_REEXEC"
	// legacyPinReexecEnv is what a pinned release from before the rename to
	// onkt checks, so it does not re-exec again.
	legacyPinReexecEnv = "ONEK_PINNED_REEXEC"
	pinSkipEnv         = "ONKT_NO_PIN"
)

var pinnedCommands = map[string]bool{commandBuild: true, "generate": true, commandCheck: true, "watch": true, "mock": true}

func pinDirectory(command string, args []string) string {
	dir := "."
	positional := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if strings.HasPrefix(arg, "-") {
			switch name {
			case "dir":
				if hasValue {
					return value
				}
				if i+1 < len(args) {
					return args[i+1]
				}
			case "format", "interval", "addr", "seed", "error-rate", "latency":
				if !hasValue {
					i++
				}
			}
			continue
		}
		if positional == "" && (command == commandBuild || command == "generate" || command == commandCheck) {
			positional = arg
		}
	}
	if positional != "" {
		return positional
	}
	return dir
}

func pinnedBinary(command string, args []string) (string, error) {
	if !pinnedCommands[command] || onek.Getenv(pinReexecEnv) != "" || onek.Getenv(pinSkipEnv) != "" {
		return "", nil
	}
	wanted, err := onek.PinnedVersion(pinDirectory(command, args))
	if err != nil {
		return "", err
	}
	if wanted == "" || wanted == onek.NormalizeVersion(version) {
		return "", nil
	}
	if version == "dev" {
		fmt.Fprintf(os.Stderr, "onkt: this is a development build, ignoring the project's version = %q\n", wanted)
		return "", nil
	}
	source := onek.ReleaseSource{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	path, err := source.Fetch(context.Background(), wanted)
	if err != nil {
		return "", fmt.Errorf("fetch onkt %s pinned by onkt.toml: %w", wanted, err)
	}
	return path, nil
}

func reexecPinned(path string, args []string) error {
	if err := os.Setenv(legacyPinReexecEnv, "1"); err != nil {
		return err
	}
	if err := os.Setenv(pinReexecEnv, "1"); err != nil {
		return err
	}
	return execBinary(path, args)
}
