package httpkit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

// Check is one named readiness probe for Health.
type Check struct {
	Name string
	Run  func(ctx context.Context) error
}

// Health answers 200 {"status":"ok"} when every check passes and 503
// {"status":"unavailable","failed":[names]} when one fails. It never sends the
// error text, which can name internal hosts.
func Health(checks ...Check) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var failed []string
		for _, check := range checks {
			if check.Run != nil {
				if err := check.Run(r.Context()); err != nil {
					failed = append(failed, check.Name)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if len(failed) > 0 {
			sort.Strings(failed)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "unavailable", "failed": failed})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

// SPAOptions configures SPA.
type SPAOptions struct {
	// Index is the document served for client-side routes. It defaults to
	// "index.html".
	Index string
	// Immutable reports whether a file name is content-hashed and can be
	// cached forever. It defaults to names under "assets/".
	Immutable func(name string) bool
	// OnServe runs before every response, with the file about to be served, to
	// set per-request headers such as a Content-Security-Policy.
	OnServe func(w http.ResponseWriter, r *http.Request, name string)
}

// SPA serves a single-page app from fsys: files that exist are served as
// they are, a path with no file extension that does not exist gets the index
// document (so client-side routes work), and a missing file with an
// extension is a 404 rather than index.html.
func SPA(fsys fs.FS, opts SPAOptions) http.Handler {
	index := opts.Index
	if index == "" {
		index = "index.html"
	}
	immutable := opts.Immutable
	if immutable == nil {
		immutable = func(name string) bool { return strings.HasPrefix(name, "assets/") }
	}
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = index
		}
		if info, err := fs.Stat(fsys, name); err != nil || info.IsDir() {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = index
		}
		if opts.OnServe != nil {
			opts.OnServe(w, r, name)
		}
		if name == index {
			w.Header().Set("Cache-Control", "no-cache")
			serveFile(w, r, fsys, index)
			return
		}
		if immutable(name) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var modTime time.Time
	if info, err := fs.Stat(fsys, name); err == nil {
		modTime = info.ModTime()
	}
	http.ServeContent(w, r, name, modTime, bytes.NewReader(data))
}

// ETag returns a strong validator for body.
func ETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// IfNoneMatch reports whether the request's If-None-Match header matches etag,
// so the handler can answer 304. It understands lists, "*" and weak (W/)
// validators.
func IfNoneMatch(r *http.Request, etag string) bool {
	header := r.Header.Get("If-None-Match")
	if header == "" || etag == "" {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == want {
			return true
		}
	}
	return false
}
