//go:build !darwin

package main

import "fmt"

func startNativeFileDrag(_ string) error {
	return fmt.Errorf("native drag is not supported on this platform")
}
