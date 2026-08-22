# Runtime libraries — design (next milestone)

Goal: stop vendoring large runtime blobs into every generation so fixes ship
by bumping a dependency instead of regenerating code.

## Why not now

onekit has no external consumers yet; regeneration is cheap and the
self-contained output keeps every generated project buildable with zero
extra dependencies. Extracting runtimes before there are consumers adds a
publishing pipeline, version-skew matrix, and import-resolution complexity
for no observed pain. This document freezes the design so implementation can
start the moment regeneration friction becomes real.

## Package layout

    runtimes/
      go/onkiterrpc/     Go module: EventStream[T], readResponseBody,
                         WSOut/WSDuplex helpers, SSE frame reader
      ts/                npm @onekit/runtime: sseResponse(), stream reader,
                         ApiError, socket duplex base classes
      python/            PyPI onekit-runtime: _read_bounded, WsFrameSocket
      rust/              crates.io onekit-runtime: EventStream, ws helpers

## Config surface

Per-target opt-in flag, default false (back-compat):

    [generate.go-client]
    out = "gen/go"
    runtime_imports = true   # import onekitrpc instead of inlining

Generators branch on the flag: identical semantics, either inlined blobs or
thin imports of the same code. Golden tests cover BOTH modes so neither
rots.

## Invariants

1. Inline mode must remain byte-compatible with today's output.
2. Runtime modules carry their own unit tests; generators test only wiring.
3. A runtime release never breaks older onekit versions: additive APIs only;
   generated imports pin minimum versions via go.mod/npm ranges emitted into
   consumer docs (generators cannot edit foreign manifests).
4. WS + SSE wire formats are frozen by conformance tests shared across all
   runtimes (see roadmap: cross-language conformance harness).

## Publishing

goreleaser already ships binaries; add npm/PyPI/crates publish jobs gated on
tags `runtime-v*`. Secrets are owner-supplied; workflows land disabled.
