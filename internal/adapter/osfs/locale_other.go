//go:build !windows

package osfs

// SystemLocale returns "": outside Windows the locale variables (LANG and
// the like) say the language, and the CLI reads them itself.
func SystemLocale() string { return "" }
