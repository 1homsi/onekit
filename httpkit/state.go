package httpkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// State is the per-request state Middleware stores in the context. It is
// shared by pointer, so values set while handling the request (the
// authenticated principal, for example) are visible to an observer that runs
// after the handler returns.
type State struct {
	RequestID string
	Start     time.Time
	ClientIP  string

	mu        sync.Mutex
	principal any
	status    int
	bytes     int64
}

type stateKey struct{}

// StateFrom returns the request state, or nil outside Middleware.
func StateFrom(ctx context.Context) *State {
	state, _ := ctx.Value(stateKey{}).(*State)
	return state
}

// WithState returns a context carrying state.
func WithState(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, stateKey{}, state)
}

// RequestID returns the request id, or "" when there is none.
func RequestID(ctx context.Context) string {
	if state := StateFrom(ctx); state != nil {
		return state.RequestID
	}
	return ""
}

// Status is the response status captured so far (0 before the header is sent).
func (s *State) Status() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Bytes is the number of response body bytes written so far.
func (s *State) Bytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytes
}

// SetPrincipal records the authenticated principal on the request state.
func SetPrincipal(ctx context.Context, principal any) {
	if state := StateFrom(ctx); state != nil {
		state.mu.Lock()
		state.principal = principal
		state.mu.Unlock()
	}
}

// Principal returns the principal recorded by SetPrincipal when it has type T.
func Principal[T any](ctx context.Context) (T, bool) {
	var zero T
	state := StateFrom(ctx)
	if state == nil {
		return zero, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	value, ok := state.principal.(T)
	return value, ok
}

// MustPrincipal is Principal for routes behind authentication: it panics when
// no principal of type T was set, which is a wiring bug, not a client error.
func MustPrincipal[T any](ctx context.Context) T {
	value, ok := Principal[T](ctx)
	if !ok {
		panic("httpkit: no principal of the requested type on this request; is the route behind authentication?")
	}
	return value
}

// Config configures Middleware.
type Config struct {
	// RequestIDHeader is read from the request and echoed on the response.
	// It defaults to "X-Request-Id".
	RequestIDHeader string
	// NewRequestID creates ids for requests that carry none. It defaults to a
	// random 16-byte hex string.
	NewRequestID func() string
	// TrustedProxies lists the addresses or prefixes (192.0.2.1, 10.0.0.0/8)
	// whose X-Forwarded-For header is believed. A request from any other peer
	// uses its own RemoteAddr.
	TrustedProxies []string
	// Now returns the current time. It defaults to time.Now.
	Now func() time.Time
}

// Middleware stores State in the request context, honors or creates the
// request id, resolves the client IP and records the response status and
// size.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	header := cfg.RequestIDHeader
	if header == "" {
		header = "X-Request-Id"
	}
	newID := cfg.NewRequestID
	if newID == nil {
		newID = randomID
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	trusted := parseProxies(cfg.TrustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := &State{RequestID: r.Header.Get(header), Start: now(), ClientIP: ClientIP(r, trusted)}
			if state.RequestID == "" {
				state.RequestID = newID()
			}
			w.Header().Set(header, state.RequestID)
			recorder := &Recorder{ResponseWriter: w, state: state}
			next.ServeHTTP(recorder, r.WithContext(WithState(r.Context(), state)))
		})
	}
}

func randomID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// Proxies is a parsed trusted-proxy list.
type Proxies struct{ prefixes []netip.Prefix }

// ParseProxies parses addresses and CIDR prefixes. Entries that do not parse
// are ignored.
func ParseProxies(entries []string) Proxies { return parseProxies(entries) }

func parseProxies(entries []string) Proxies {
	var out Proxies
	for _, entry := range entries {
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			out.prefixes = append(out.prefixes, prefix)
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			out.prefixes = append(out.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return out
}

func (p Proxies) trusts(addr netip.Addr) bool {
	for _, prefix := range p.prefixes {
		if prefix.Contains(addr.Unmap()) || prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP returns the caller's address: the peer address of the connection,
// or, when that peer is a trusted proxy, the closest X-Forwarded-For entry
// that is not itself a trusted proxy.
func ClientIP(r *http.Request, trusted Proxies) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !trusted.trusts(peer) {
		return host
	}
	forwarded := r.Header.Values("X-Forwarded-For")
	var chain []string
	for _, value := range forwarded {
		chain = append(chain, splitComma(value)...)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(chain[i])
		if err != nil {
			return host
		}
		if !trusted.trusts(addr) {
			return addr.String()
		}
	}
	return host
}

func splitComma(value string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(value); i++ {
		if i == len(value) || value[i] == ',' {
			part := trimSpace(value[start:i])
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
