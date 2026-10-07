package httpkit

import "net/http"

// Recorder wraps a ResponseWriter and records the status and the number of
// body bytes. Middleware installs one; it also works standalone.
type Recorder struct {
	http.ResponseWriter
	state       *State
	status      int
	bytes       int64
	wroteHeader bool
}

// NewRecorder wraps w.
func NewRecorder(w http.ResponseWriter) *Recorder {
	return &Recorder{ResponseWriter: w}
}

// Status is the status code sent, or 200 once a body was written without one,
// or 0 before anything was sent.
func (r *Recorder) Status() int { return r.status }

// Bytes is the number of body bytes written.
func (r *Recorder) Bytes() int64 { return r.bytes }

func (r *Recorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	if r.state != nil {
		r.state.mu.Lock()
		r.state.status = status
		r.state.mu.Unlock()
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *Recorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += int64(n)
	if r.state != nil {
		r.state.mu.Lock()
		r.state.bytes += int64(n)
		r.state.mu.Unlock()
	}
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *Recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Flush implements http.Flusher when the underlying writer does.
func (r *Recorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
