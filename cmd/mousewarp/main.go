// Mousewarp — exercises glfw.SetCursorPos.
//
// Dungeoncrawl uses CursorMode=Disabled which auto-recenters the cursor
// internally; nothing in the suite actively *warps* the cursor via the
// public API.  This program does: each frame it reads the cursor
// position, computes the delta from window centre, applies it to a
// virtual ball's velocity, and then warps the cursor back to the
// centre with SetCursorPos so deltas don't accumulate.
//
// The same pattern is what FPS games used before raw mouse input
// existed; modern games use CursorDisabled, but the manual-warp path
// has to keep working for source-compat with older code.
//
// Controls
//   Mouse motion : steers the ball
//   Click        : reset the ball to centre
//   F11          : fullscreen
//   Esc          : quit
package main

import (
	"fmt"
	"math"
	"runtime"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/render"
	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/winutil"
)

const (
	winW = 700
	winH = 500
)

func init() { runtime.LockOSThread() }

type ball struct {
	x, y   float64
	vx, vy float64
}

func main() {
	if err := glfw.Init(); err != nil {
		panic(err)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Resizable, glfw.False)

	win, err := glfw.CreateWindow(winW, winH, "Mouse Warp", nil, nil)
	if err != nil {
		panic(err)
	}
	win.MakeContextCurrent()
	glfw.SwapInterval(1)
	if err := gl.Init(); err != nil {
		panic(err)
	}

	// Hide the cursor so the user doesn't see it teleporting back to
	// the centre every frame — that would feel terrible.  Note: we use
	// CursorHidden, not CursorDisabled.  Disabled would handle the
	// recentering automatically; we want to test the manual SetCursorPos
	// path explicitly.
	win.SetInputMode(glfw.CursorMode, glfw.CursorHidden)

	r := render.New()
	defer r.Destroy()

	b := &ball{x: winW / 2, y: winH / 2}
	warpCount := 0
	totalDX := 0.0
	totalDY := 0.0

	// Snap the cursor to the centre once at startup so the first frame's
	// delta is zero.
	win.SetCursorPos(winW/2, winH/2)

	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press {
			return
		}
		switch key {
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		case glfw.KeyF11:
			winutil.ToggleFullscreen(win)
		}
	})
	win.SetMouseButtonCallback(func(_ *glfw.Window, btn glfw.MouseButton, action glfw.Action, _ glfw.ModifierKey) {
		if btn == glfw.MouseButtonLeft && action == glfw.Press {
			b.x, b.y = winW/2, winH/2
			b.vx, b.vy = 0, 0
		}
	})

	for !win.ShouldClose() {
		// Read where the OS thinks the cursor is, compute delta from
		// centre, apply it, then warp back.  This is the heart of the
		// test: SetCursorPos has to actually move the cursor reliably
		// every frame or the deltas will compound and the ball will
		// fly off in one direction.
		mx, my := win.GetCursorPos()
		dx := mx - winW/2
		dy := my - winH/2
		if dx != 0 || dy != 0 {
			b.vx += dx * 0.5
			b.vy += dy * 0.5
			totalDX += math.Abs(dx)
			totalDY += math.Abs(dy)
			win.SetCursorPos(winW/2, winH/2)
			warpCount++
		}

		// Friction + integrate.
		b.vx *= 0.92
		b.vy *= 0.92
		b.x += b.vx * 0.016
		b.y += b.vy * 0.016
		// Wrap toroidally so the ball is never lost.
		if b.x < 0 {
			b.x += winW
		}
		if b.x >= winW {
			b.x -= winW
		}
		if b.y < 0 {
			b.y += winH
		}
		if b.y >= winH {
			b.y -= winH
		}

		gl.ClearColor(0.06, 0.07, 0.10, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)

		// Crosshair at centre.
		r.Rect(winW/2-1, winH/2-12, 2, 24, 0.30, 0.32, 0.40)
		r.Rect(winW/2-12, winH/2-1, 24, 2, 0.30, 0.32, 0.40)

		// Ball.
		r.Rect(float32(b.x)-8, float32(b.y)-8, 16, 16, 0.95, 0.85, 0.30)

		// Stats.
		stat := fmt.Sprintf("WARPS %d", warpCount)
		r.Text(12, 12, 11, 16, 0, stat, 0.7, 0.85, 1)
		dxs := fmt.Sprintf("DX %d", int(totalDX))
		r.Text(12, 36, 11, 16, 0, dxs, 0.6, 0.7, 0.85)
		dys := fmt.Sprintf("DY %d", int(totalDY))
		r.Text(12, 60, 11, 16, 0, dys, 0.6, 0.7, 0.85)

		win.SwapBuffers()
		glfw.PollEvents()
	}
}
