package genrust

import (
	"flag"
	"os"
	"runtime"
	"strconv"
	"testing"
)

const maxParallelCargo = 3

func TestMain(m *testing.M) {
	flag.Parse()
	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "test.parallel" {
			explicit = true
		}
	})
	if !explicit && runtime.GOMAXPROCS(0) > maxParallelCargo {
		_ = flag.Set("test.parallel", strconv.Itoa(maxParallelCargo))
	}
	os.Exit(m.Run())
}
