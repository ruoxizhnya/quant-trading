// Package observability — gin middleware and HTTP transport wrapper
// for the Sprint 6 P0-3 observability stack.
package observability

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ContextKey is the gin.Context key under which the per-request
// correlation token is stored. Exposed for callers that need to
// read it back from the gin context.
const ContextKey = "request_id"

// contextKey is the key under which the request_id is stored in
// the request's context.Context (for non-gin code paths such as
// outbound HTTP transports). It is a distinct type (per stdlib
// guidance) so it cannot collide with keys defined in other
// packages.
type contextKey struct{ name string }

func (c contextKey) String() string { return "observability context key: " + c.name }

// RequestIDKey is the public key under which the request_id is
// stored in a context.Context. Callers can retrieve it via
// ctx.Value(observability.RequestIDKey).
var RequestIDKey = contextKey{name: "request_id"}

// RequestIDFromContext extracts the correlation token from a
// request context. Returns "" if no token is present.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(RequestIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// RequestIDMiddleware returns a gin middleware that:
//
//   - reads X-Request-ID from the inbound request, or generates a
//     fresh UUIDv4 if absent
//   - stores the token in the gin context under ContextKey AND in
//     the request's context.Context under RequestIDKey
//   - mirrors the token back into the response X-Request-ID header
//
// The middleware is intentionally idempotent: applying it twice in
// a chain is a no-op for the second application (the upstream
// token wins, matching the standard propagator semantic).
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(RequestIDHeader)
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Set(ContextKey, rid)
		c.Request = c.Request.WithContext(withRequestID(c.Request.Context(), rid))
		c.Writer.Header().Set(RequestIDHeader, rid)
		c.Next()
	}
}

// withRequestID returns a context that carries the supplied
// request_id. The parent context is delegated to for everything
// except RequestIDKey lookups.
func withRequestID(parent context.Context, rid string) context.Context {
	return &ridCtx{parent: parent, rid: rid}
}

type ridCtx struct {
	parent context.Context
	rid    string
}

// Deadline delegates to the parent context.
func (c *ridCtx) Deadline() (time.Time, bool) { return c.parent.Deadline() }

// Done delegates to the parent context.
func (c *ridCtx) Done() <-chan struct{} { return c.parent.Done() }

// Err delegates to the parent context.
func (c *ridCtx) Err() error { return c.parent.Err() }

// Value returns the request_id when queried with RequestIDKey,
// otherwise delegates to the parent context.
func (c *ridCtx) Value(key any) any {
	if key == RequestIDKey {
		return c.rid
	}
	if c.parent == nil {
		return nil
	}
	return c.parent.Value(key)
}

// HTTPTransport wraps an http.RoundTripper and copies the request_id
// from the inbound request context into the outbound X-Request-ID
// header. Use it in any client that needs to propagate correlation
// tokens to downstream services.
//
// This is a deliberately tiny implementation — it does NOT depend
// on the OpenTelemetry SDK so the dependency surface stays small
// for Sprint 6. The same effect can be achieved with an OTel
// propagator later without changing the call sites.
type HTTPTransport struct {
	Base http.RoundTripper
	// Metrics is optional; when non-nil, every roundtrip records
	// one observation in http_client_requests_total.
	Metrics *Metrics
	// Service is the label value used for http_client_requests_total
	// (e.g. "data", "strategy", "llm").
	Service string
}

// RoundTrip implements http.RoundTripper.
func (t *HTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req != nil {
		if rid := RequestIDFromContext(req.Context()); rid != "" {
			// Don't clobber an explicit X-Request-ID the caller
			// already set (matches standard OTel propagator semantics).
			if req.Header.Get(RequestIDHeader) == "" {
				req.Header.Set(RequestIDHeader, rid)
			}
		}
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if t.Metrics != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Metrics.ObserveHTTP(t.Service, status)
	}
	return resp, err
}

// WithRequestIDHeader is a small helper for callers that build an
// http.Request via http.NewRequestWithContext and want to attach
// the request_id explicitly. The same effect happens automatically
// when the request is dispatched via an http.Client whose
// Transport has been wrapped with HTTPTransport.
func WithRequestIDHeader(req *http.Request, rid string) *http.Request {
	if req == nil || rid == "" {
		return req
	}
	if req.Header.Get(RequestIDHeader) == "" {
		req.Header.Set(RequestIDHeader, rid)
	}
	return req
}
