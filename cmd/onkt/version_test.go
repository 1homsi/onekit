package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	module := func(v string) *debug.BuildInfo { return &debug.BuildInfo{Main: debug.Module{Version: v}} }
	tests := []struct {
		stamped string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"v0.18.0", module("v0.17.0"), true, "v0.18.0"},
		{"dev", module("v0.18.0"), true, "v0.18.0"},
		{"dev", module("(devel)"), true, "dev"},
		{"dev", nil, false, "dev"},
	}
	for _, tt := range tests {
		if got := resolveVersion(tt.stamped, tt.info, tt.ok); got != tt.want {
			t.Fatalf("resolveVersion(%q, %v) = %q, want %q", tt.stamped, tt.info, got, tt.want)
		}
	}
}
