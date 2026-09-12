//go:build darwin && cgo && fileprovider && !bindings

package fpnative

/*
#cgo CFLAGS: -x objective-c -fblocks -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework FileProvider -framework Security -framework AppKit
#include <stdlib.h>
char *mahpastes_fp_call(const char *request);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

func call(request []byte) (Result, error) {
	in := C.CString(string(request))
	defer C.free(unsafe.Pointer(in))
	out := C.mahpastes_fp_call(in)
	if out == nil {
		return Result{}, errors.New("File Provider bridge returned no result")
	}
	defer C.free(unsafe.Pointer(out))
	var result Result
	if err := json.Unmarshal([]byte(C.GoString(out)), &result); err != nil {
		return result, err
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
}
