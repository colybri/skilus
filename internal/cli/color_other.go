//go:build !windows

package cli

import "os"

// enableColor reports whether f understands ANSI colors; every terminal
// outside Windows does.
func enableColor(*os.File) bool { return true }
