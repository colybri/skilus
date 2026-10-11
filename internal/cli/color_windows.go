package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableColor turns on ANSI sequences in the Windows console and reports
// whether it understands them; old consoles do not.
func enableColor(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
