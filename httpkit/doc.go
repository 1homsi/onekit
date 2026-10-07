// Package httpkit is the glue an application usually writes around an
// onek-generated Go server: an error envelope, request state in the context,
// a response recorder and guard resolution. It depends only on the standard
// library and never imports generated code, so it works with every generated
// package, inline or shared runtime.
package httpkit

const defaultRequestIDHeader = "X-Request-Id"
