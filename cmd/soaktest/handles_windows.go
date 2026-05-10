//go:build windows

package main

import "golang.org/x/sys/windows"

// guiResources returns the calling process's GDI and USER object counts
// via GetGuiResources.  Both have a 10,000-handle-per-process ceiling on
// stock Windows, so a slow upward trend over hours is a leak.
func guiResources() (gdi, usr uint32) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("GetGuiResources")
	hProcess := uintptr(windows.CurrentProcess())

	g, _, _ := proc.Call(hProcess, 0) // GR_GDIOBJECTS
	u, _, _ := proc.Call(hProcess, 1) // GR_USEROBJECTS
	return uint32(g), uint32(u)
}
