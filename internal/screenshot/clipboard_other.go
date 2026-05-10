//go:build !windows

package screenshot

import "errors"

// CopyToClipboard is a no-op stub on non-Windows platforms.  Image
// clipboards on macOS (NSPasteboard) and Linux (X11 selections /
// wl_data_source) need platform-specific code that isn't required
// for the suite's test goals; consumers should fall back to Save().
func CopyToClipboard(w, h int) error {
	return errors.New("CopyToClipboard: only implemented on Windows")
}
