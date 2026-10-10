package osfs

import (
	"syscall"
	"unsafe"
)

// SystemLocale returns the user's locale name, such as es-ES. Windows rarely
// sets LANG, so this is how skilus follows the language of the system.
func SystemLocale() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	if proc.Find() != nil {
		return ""
	}
	buf := make([]uint16, 85) // LOCALE_NAME_MAX_LENGTH
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
