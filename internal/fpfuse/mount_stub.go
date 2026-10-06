//go:build !linux || bindings

package fpfuse

import (
	"context"
	"errors"

	"go-clipboard/internal/fileprovider"
)

var errUnsupported = errors.New("file manager integration is unavailable in this build")

func Supported() bool { return false }

func Available() error { return errUnsupported }

type Mount struct{}

func Start(context.Context, *fileprovider.Store, string) (*Mount, error) {
	return nil, errUnsupported
}

func (*Mount) Dir() string { return "" }

func (*Mount) Alive() bool { return false }

func (*Mount) Created() bool { return false }

func (*Mount) Close() {}
