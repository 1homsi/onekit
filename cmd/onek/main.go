package main

import (
	"fmt"
	"os"

	"github.com/1homsi/onekit/internal/onek"
)

const (
	minArgs  = 2
	dirArgAt = 3
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: onek <build|check|generate> [dir]\n       onek compat <previous-dir> <current-dir>")
}

func main() {
	if len(os.Args) < minArgs {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "build", "generate":
		dir := argumentOrDot()
		err = onek.Build(dir)
	case "check":
		dir := argumentOrDot()
		err = onek.Check(dir)
	case "compat":
		if len(os.Args) != 4 {
			usage()
			os.Exit(1)
		}
		findings, compatErr := onek.Compatibility(os.Args[2], os.Args[3])
		if compatErr != nil {
			err = compatErr
			break
		}
		for _, finding := range findings {
			_, _ = fmt.Fprintln(os.Stdout, finding.Path+": "+finding.Message)
		}
		if len(findings) > 0 {
			os.Exit(2)
		}
	case "fmt":
		fmt.Fprintln(os.Stderr, "onek fmt: not yet implemented")
		os.Exit(1)
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "onek:", err)
		os.Exit(1)
	}
}

func argumentOrDot() string {
	if len(os.Args) >= dirArgAt {
		return os.Args[2]
	}
	return "."
}
