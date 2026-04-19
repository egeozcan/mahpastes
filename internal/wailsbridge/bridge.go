// Package wailsbridge is the single importer of github.com/wailsapp/wails/v2/pkg/runtime
// in this repo. All runtime interactions (events, dialogs) route through a *Bridge
// value owned by *App. This lets the Wails v2 -> v3 migration swap this package's
// internals without touching any call site.
//
// DO NOT import wails/v2/pkg/runtime anywhere else in this module.
package wailsbridge

import (
	"context"

	rt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Bridge wraps the Wails v2 runtime. On v3 swap, replace `ctx` with an
// `*application.App` handle and rewrite each method body; call sites don't change.
//
// All methods are nil-safe: a nil *Bridge or a Bridge with a nil ctx is a no-op
// for event/dialog calls. This preserves the existing test patterns where
// runtime calls were skipped when the ctx hadn't been set yet (e.g., during
// unit tests that don't run Wails).
type Bridge struct {
	ctx context.Context
}

// New constructs a Bridge from the context delivered by Wails OnStartup.
func New(ctx context.Context) *Bridge {
	return &Bridge{ctx: ctx}
}

// active reports whether the bridge is wired up enough to perform a runtime
// call. Centralizing this keeps every method's nil-safety identical.
func (b *Bridge) active() bool {
	return b != nil && b.ctx != nil
}

// Emit dispatches an event to the frontend. No-op if the bridge isn't active.
func (b *Bridge) Emit(name string, data ...any) {
	if !b.active() {
		return
	}
	rt.EventsEmit(b.ctx, name, data...)
}

// On subscribes to an event from the frontend. The callback runs on the
// runtime's dispatcher goroutine — don't block. No-op if the bridge isn't active.
func (b *Bridge) On(name string, cb func(data ...any)) {
	if !b.active() {
		return
	}
	rt.EventsOn(b.ctx, name, cb)
}

// Once subscribes for a single event. No-op if the bridge isn't active.
func (b *Bridge) Once(name string, cb func(data ...any)) {
	if !b.active() {
		return
	}
	rt.EventsOnce(b.ctx, name, cb)
}

// Off removes all handlers for the named event. No-op if the bridge isn't active.
func (b *Bridge) Off(name string) {
	if !b.active() {
		return
	}
	rt.EventsOff(b.ctx, name)
}
