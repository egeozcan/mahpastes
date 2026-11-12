//go:build !unix

package desktopprefs

import "os"

// openFlags opens the prefs file for reading.
const openFlags = os.O_RDONLY
