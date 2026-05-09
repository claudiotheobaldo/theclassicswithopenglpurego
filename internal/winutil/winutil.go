// Package winutil holds small window-management helpers shared by every
// game in the suite.  Currently: fullscreen toggle and aspect-fit
// viewport rectangles.
package winutil

import (
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"
)

// savedFrame remembers a window's pre-fullscreen position and size so we
// can restore it when toggling back.
type savedFrame struct {
	x, y, w, h int
}

var savedFrames = map[*glfw.Window]*savedFrame{}

// ToggleFullscreen flips a window between borderless fullscreen on the
// primary monitor and its previous windowed geometry.
//
// When entering fullscreen we snapshot the current pos/size so the next
// toggle restores both.  Uses the monitor's native video mode so the
// desktop resolution doesn't change.
func ToggleFullscreen(win *glfw.Window) {
	if win.GetMonitor() != nil {
		// Currently fullscreen → restore.
		s, ok := savedFrames[win]
		if !ok {
			// We weren't the ones who fullscreened it; pick a sane default.
			s = &savedFrame{x: 100, y: 100, w: 800, h: 600}
		}
		win.SetMonitor(nil, s.x, s.y, s.w, s.h, 0)
		delete(savedFrames, win)
		return
	}
	// Currently windowed → snapshot and enter fullscreen.
	x, y := win.GetPos()
	w, h := win.GetSize()
	savedFrames[win] = &savedFrame{x: x, y: y, w: w, h: h}
	mon := glfw.GetPrimaryMonitor()
	if mon == nil {
		return
	}
	vm := mon.GetVideoMode()
	if vm == nil {
		return
	}
	win.SetMonitor(mon, 0, 0, vm.Width, vm.Height, vm.RefreshRate)
}

// LetterboxRect returns the largest (w, h) rect with the given virtual
// aspect that fits inside the framebuffer (fbW, fbH), centred.  Use it
// to feed gl.Viewport so a fixed-aspect game doesn't stretch when the
// window changes shape (resize, fullscreen on a wider monitor, etc.).
func LetterboxRect(fbW, fbH, virtualW, virtualH int) (x, y, w, h int) {
	if fbW <= 0 || fbH <= 0 || virtualW <= 0 || virtualH <= 0 {
		return 0, 0, fbW, fbH
	}
	targetAspect := float64(virtualW) / float64(virtualH)
	fbAspect := float64(fbW) / float64(fbH)
	if fbAspect > targetAspect {
		// Framebuffer is wider than target — bars on the sides.
		h = fbH
		w = int(float64(fbH) * targetAspect)
	} else {
		w = fbW
		h = int(float64(fbW) / targetAspect)
	}
	x = (fbW - w) / 2
	y = (fbH - h) / 2
	return
}
