package bridgeiface

import "context"

// Bridge is the event surface shared by the desktop Wails bridge and the
// headless server's browser event stream.
type Bridge interface {
	Emit(name string, data ...any)
	On(name string, cb func(data ...any))
}

// NoOp discards events and ignores subscriptions.
type NoOp struct{}

func (NoOp) Emit(name string, data ...any)        {}
func (NoOp) On(name string, cb func(data ...any)) {}

// SSEAuth carries per-connection authorization metadata for an open server-mode
// event stream. The headless API layer stashes it in the request context before
// delegating to the SSE broker, which uses it to enforce per-key connection caps
// and to re-validate the key/session for the life of the stream (so a revoked
// key or expired session stops streaming instead of running until disconnect).
//
// It lives here, in the dependency-free bridge package, so the API layer
// (package app) and the SSE broker (package webui) can share it without either
// importing the other.
type SSEAuth struct {
	// KeyID identifies the authenticated key for per-key connection accounting.
	KeyID int64
	// Revalidate reports whether the connection is still authorized. The broker
	// calls it periodically; returning false terminates the stream.
	Revalidate func() bool
}

type sseAuthKeyType struct{}

var sseAuthKey sseAuthKeyType

// WithSSEAuth returns a copy of ctx carrying the SSE connection's auth metadata.
func WithSSEAuth(ctx context.Context, a *SSEAuth) context.Context {
	return context.WithValue(ctx, sseAuthKey, a)
}

// SSEAuthFrom extracts the SSE auth metadata previously stored with WithSSEAuth,
// or nil if none was set.
func SSEAuthFrom(ctx context.Context) *SSEAuth {
	a, _ := ctx.Value(sseAuthKey).(*SSEAuth)
	return a
}
