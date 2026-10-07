# Moving a gin API onto onekit

This note records, capability by capability, what onekit provides for the routes a gin service keeps for itself, the schema to write, and which routes are better left as plain `net/http` handlers.

The rule of thumb: if a route has one verb and a path, onekit can describe it. When the request or response is not a JSON message, declare the route with `@http` and write the handler by hand. When the route does not fit a single verb and path at all, register a plain handler with `api.Mount`. In both cases the route goes through the same middleware, authorizer, request id, error writer and request observer as a generated route.

## What was added, per capability

| # | Capability | Answer |
| --- | --- | --- |
| 1 | Raw and binary responses | `@http("content/type")` route; the handler writes headers and body. |
| 2 | Raw and streamed request bodies | `@http` route with a body verb and `@max_body`; the handler reads `r.Body`. The any-method proxy stays a plain handler, mounted with `api.Mount`. |
| 3 | Conditional responses | `api.NotModified(ctx)` plus `api.ResponseHeader(ctx)`; `httpkit.ETag` and `httpkit.IfNoneMatch`. |
| 4 | Headers, cookies, redirects, status codes | `api.SetCookie`, `api.Redirect`, `api.SetResponseStatus`, `@success(204)`, `httpkit.SetFlag` for a per-request audit skip. |
| 5 | Free-form request and response bodies | Already supported: `json` fields, `@body("field")`, `@unwrap`, `@object`. Now covered by a test. |
| 6 | Non-REST and non-schema routes | `httpkit.Health` and `httpkit.SPA` helpers; host resolution stays a plain middleware. All wrap with `api.Mount` or `httpkit.Middleware`. |
| 7 | Auth and audit parity for hand-registered handlers | `api.Mount` with a `RequestMetadata` (Meta, Guards, Scopes); the observer gets the request, bytes, path values and panics; `@query` on body routes. |

### 1. Raw and binary responses

```onk
message BundleRequest { slug: string  v: int32 @query }
message Nothing {}

service Bundles {
  base_path: "/apps"
  bundle(BundleRequest) -> Nothing @get("/{slug}/bundle.js") @http("application/javascript") @guard("apps/read/:slug") @meta("audit.event", "bundle.get")
}
```

The handler sets `Content-Type`, `ETag`, `Cache-Control` and any `X-Detool-*` header, calls `w.WriteHeader(304)` for a conditional hit, or streams with `io.Copy`. Slot limiting is ordinary handler code. Go clients get an `*http.Response`; TypeScript clients get a `Response` (`await res.blob()`, `res.headers.get("ETag")`). The response message is only documentation: use any message, `Nothing` here.

### 2. Raw and streamed request bodies

```onk
message ObjectRequest { slug: string  resource: string  key: string }
message Nothing {}

service Objects {
  base_path: "/apps"
  put_object(ObjectRequest) -> Nothing @put("/{slug}/s3/{resource}/objects/{key...}") @http @max_body("256MiB") @meta("audit.event", "s3.put")
}
```

`r.Body` is already wrapped in `http.MaxBytesReader` with the `@max_body` limit (or the `WithMaxRequestBodyBytes` value), so `io.Copy(s3Writer, r.Body)` streams. Clients take the body: `client.PutObject(ctx, req, body io.Reader, contentType)` in Go and `client.putObject(req, { body, contentType })` in TypeScript.

**Left as a plain handler: the resource proxy** (any method on `/apps/{slug}/proxy/{resource}/{path...}`). An onekit route has exactly one verb, and the proxy must forward whatever method arrives and return the upstream status, headers and body untouched. Registering a handler without a method gives that:

```go
api.Mount(mux, "/apps/{slug}/proxy/{resource}/{path...}", proxyHandler, api.RequestMetadata{
    Service: "Proxy", Method: "Forward", Route: "/apps/{slug}/proxy/{resource}/{path...}",
    Meta:   map[string]string{"audit.event": "proxy.forward"},
    Guards: []string{"apps/use/:slug"},
}, opts...)
```

Uploads and local-execution routes that take a binary or streamed body are `@http` routes like `put_object`.

### 3. Conditional responses

A JSON route stays a normal route:

```onk
files(FilesRequest) -> FilesResponse @get("/apps/{slug}/files")
```

```go
func (s *service) Files(ctx context.Context, req *api.FilesRequest) (*api.FilesResponse, error) {
    files, etag, err := s.files.List(ctx, req.Slug)
    if err != nil {
        return nil, err
    }
    h := api.ResponseHeader(ctx)
    h.Set("ETag", etag)
    h.Set("Cache-Control", "private, max-age=0, must-revalidate")
    if r, ok := api.HTTPRequestFromContext(ctx); ok && httpkit.IfNoneMatch(r, etag) {
        api.NotModified(ctx)
        return &api.FilesResponse{}, nil
    }
    return &api.FilesResponse{Files: files}, nil
}
```

The 200 case uses the generated JSON encoding. The 304 sends only the headers.

### 4. Headers, cookies, redirects and status codes

```onk
login(LoginRequest) -> Session @post("/login")
oauth_start(Nothing) -> Nothing @get("/auth/google/start")
page_view(PageViewRequest) -> Nothing @post("/apps/{slug}/page-views") @success(204)
```

```go
api.SetCookie(ctx, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400})
api.Redirect(ctx, authURL, http.StatusFound)          // Location header, no body
httpkit.SetFlag(ctx, "skip-audit")                     // read in the observer with state.Flag("skip-audit")
```

`@success(204)` documents and sends 204 with no body. Logging out clears the cookie with `MaxAge: -1`.

### 5. Free-form request and response bodies

```onk
message RunRequest { slug: string  id: string  input: json @object }
message RunResult  { value: json @unwrap }

service Queries {
  base_path: "/apps"
  run(RunRequest) -> RunResult @post("/{slug}/queries/{id}/run") @body("input")
}
```

`input` reaches the handler as `json.RawMessage` byte for byte (key order kept, `@object` rejects an array or scalar). `value` is written as the whole response body, so a handler can return `null`, an array, a string or an object, and `RunResult{}` writes `null`. For a field inside a message, an explicit `null` is preserved and an unset field is omitted, which keeps "null versus absent" meaningful.

### 6. Non-REST and non-schema routes

Left as plain handlers (they have no request or response shape worth describing):

```go
mux.Handle("GET /healthz", httpkit.Health(httpkit.Check{Name: "db", Run: db.PingContext}))
mux.Handle("/", httpkit.SPA(dist, httpkit.SPAOptions{OnServe: embedPolicy}))
```

Custom-domain host resolution is an `http.Handler` middleware; install it with `WithMiddleware`. `httpkit.Middleware` is a plain `func(http.Handler) http.Handler`, so it wraps any handler: request id, client IP, response status and size, principal. To give these routes the authorizer and observer too, register them with `api.Mount`.

### 7. Auth and audit parity

Routes in the schema carry `@guard`, `@meta` and `@requires`, which reach the authorizer through `RequestMetadata`. Hand-registered routes carry the same data in the metadata passed to `api.Mount`, so one authorizer and one observer serve both.

The audit middleware becomes a `RequestObserver`:

```go
func (a *audit) RequestStarted(ctx context.Context, m api.RequestMetadata) context.Context {
    r, _ := api.HTTPRequestFromContext(ctx)
    ctx, _ = httpkit.EnsureState(ctx, r, httpkit.Config{TrustedProxies: a.proxies})
    return ctx
}

func (a *audit) RequestFinished(ctx context.Context, m api.RequestMetadata, res api.RequestResult) {
    state := httpkit.StateFrom(ctx)
    if state.Flag("skip-audit") {
        return
    }
    a.log(m.MetaValue("audit.event"), state.Principal(), res.Request.Method, res.Request.URL.Path, res.PathValues, res.StatusCode, res.Bytes, res.Panic != nil, state.RequestID)
}
```

`RequestResult.StatusCode` is 500 when the handler panicked and had not sent a status yet; the panic itself continues up the stack.

## Cheat sheet by route group

| Route group | Schema |
| --- | --- |
| JS bundles, draft bundle, reusable-component bundle | `@get(...) @http("application/javascript")`; the draft POST is `@post(...) @http("application/javascript") @max_body(...)` |
| Favicon, S3 download | `@get(...) @http` (set `Content-Type` and `Content-Length` from the object, `io.Copy`) |
| S3 upload | `@put(".../objects/{key...}") @http @max_body("...")` |
| Resource proxy (any method) | Plain handler through `api.Mount` with a method-less pattern |
| Uploads, local execution | `@post(...) @http @max_body("...")` |
| Files with ETag and 304 | Normal JSON route, `ResponseHeader` + `NotModified` |
| Login, accept-invite, logout | Normal JSON route, `SetCookie` (clear with `MaxAge: -1`) |
| OAuth start and callback | `@get(...)` routes with `@query` fields; `SetCookie` for state, `Redirect(ctx, url, 302)` |
| Page views | `@post(...) @success(204)` and `httpkit.SetFlag(ctx, "skip-audit")` |
| Query run and run-script | `input: json @object` with `@body("input")`, response `value: json @unwrap` |
| Scope references | Response `value: json @unwrap` (null preserved) or a message field of type `json` |
| Health | `httpkit.Health` on the mux |
| SPA and custom domains | `httpkit.SPA` plus a host-resolving middleware; `api.Mount` if they need the authorizer |

## Limits worth knowing

- `@http` is supported by the go-server, go-client, ts-client and openapi targets. A project that also generates python, dart, swift, rust or ts-server targets is told so at build time.
- `@query` on POST, PUT and PATCH has the same target support.
- The mock server answers `@http` routes with an empty JSON object.
- `Mount` registers on a `*http.ServeMux`; the pattern syntax is the standard library's.
