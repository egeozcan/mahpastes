//go:build !darwin || !cgo || !fileprovider || bindings

package fpnative

import "errors"

func call(request []byte) (Result, error) {
	return Result{}, errors.New("Finder integration is unavailable in this build")
}
