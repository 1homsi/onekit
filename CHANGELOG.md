# Changelog

## v0.3.0 - 2026-07-03

- Normalized the Go module, examples, generated fixtures, and release metadata to `github.com/stackxio/onekit`.
- Added fail-fast validation for Go JSON encoding annotations that would otherwise generate duplicate `MarshalJSON`/`UnmarshalJSON` methods.
- Added generated Go HTTP server request body limits with `DefaultMaxRequestBytes` and `WithMaxRequestBytes`.
- Expanded CI integration coverage across all shipped generators, including Go build, TypeScript type-check, and Python byte-compile checks for generated output.
- Reworded coverage reporting so low informational coverage is reported as `LOW`, not as a false test failure.
- Improved mock server generation for repeated messages, repeated scalars, enums, bytes, maps, optional scalar fields, and remaining numeric kinds.
