//go:build !linux

package main

func surfaceMount(string) (gtk, kde bool, err error) { return false, false, nil }
func unsurfaceMount(string, bool, bool) error        { return nil }
