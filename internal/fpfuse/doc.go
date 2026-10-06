// Package fpfuse serves the read-only File Provider projection as a FUSE
// filesystem on Linux, the counterpart of the macOS Finder location. The app
// process serves the mount itself; there is no extension, loopback transport
// or credential, and the mount exists only while the app runs.
package fpfuse
