package bridgeiface

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
