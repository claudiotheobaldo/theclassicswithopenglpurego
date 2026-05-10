//go:build !windows

package main

// guiResources is a no-op stub on non-Windows platforms — no equivalent
// per-process handle limit to monitor.
func guiResources() (gdi, usr uint32) { return 0, 0 }
