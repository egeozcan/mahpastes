//go:build !unix

package openrequest

// checkSocketOwner has nothing to check outside Unix: on Windows both the data
// directory and the fallback temp directory are per-user.
func checkSocketOwner(string) error { return nil }
