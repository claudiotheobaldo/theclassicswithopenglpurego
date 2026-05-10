// Dashboard — window-feature playground.
//
// Drives every glfw-purego method that didn't fit any of the games:
// Iconify / Restore / Maximize, RequestAttention, SetOpacity, SetTitle,
// the standard cursors (arrow / ibeam / crosshair / hand / hresize /
// vresize), CreateCursor with a custom image, and Monitor.SetGammaRamp.
//
// The event loop is built on glfw.WaitEvents instead of PollEvents — the
// program redraws only when something happens, which is the idiomatic
// path for desktop UIs.  A small goroutine pokes PostEmptyEvent on a
// 250 ms ticker so the status footer ("last redraw N ms ago") stays
// honest without spinning a render thread.
//
// Controls are mouse-driven.  Hover over the cursor strip on the right
// to see Win32's standard cursors switch live.  Drag the sliders.  Press
// Esc to quit.
package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"runtime"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/render"
	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/winutil"
)

const (
	winW = 760
	winH = 620
)

func init() { runtime.LockOSThread() }

// ─── Widget kinds ────────────────────────────────────────────────────────────

type rect struct{ x, y, w, h float32 }

func (r rect) contains(px, py float64) bool {
	return float32(px) >= r.x && float32(px) < r.x+r.w &&
		float32(py) >= r.y && float32(py) < r.y+r.h
}

type button struct {
	rect
	label   string
	action  func()
	primary bool // accent colour for emphasis
}

type slider struct {
	rect
	label   string
	value   float64
	min     float64
	max     float64
	onLive  func(float64) // called continuously while dragging
	dragOff float32       // x-offset of click within handle
}

type hoverRegion struct {
	rect
	label  string
	cursor *glfw.Cursor
	active bool // whether this is currently the set cursor
}

// ─── Application state ───────────────────────────────────────────────────────

type app struct {
	win     *glfw.Window
	mouseX  float64
	mouseY  float64
	mouseLB bool
	hoverID int // -1 none, otherwise hover-region index
	dragSL  *slider

	// widgets
	buttons      []*button
	sliders      []*slider
	hoverRegions []*hoverRegion

	// cursor objects (created once, freed at exit)
	standardCursors map[string]*glfw.Cursor
	customCursor    *glfw.Cursor
	currentCursor   *glfw.Cursor

	// gamma restore-on-exit
	gammaTarget *glfw.Monitor
	gammaSaved  *glfw.GammaRamp

	// status
	frames     int
	lastInput  time.Time
	cursorRowY float32 // y of the cursor-strip row (set by buildWidgets)
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

	win, err := glfw.CreateWindow(winW, winH, "Dashboard", nil, nil)
	if err != nil {
		panic(err)
	}
	win.MakeContextCurrent()
	glfw.SwapInterval(1)
	if err := gl.Init(); err != nil {
		panic(err)
	}

	r := render.New()
	defer r.Destroy()

	a := &app{win: win, hoverID: -1, lastInput: time.Now()}
	a.buildWidgets()
	a.makeCursors()
	defer a.releaseCursors()

	// Snapshot the primary monitor's gamma ramp so we can restore it
	// when we quit (otherwise the dashboard's slider would leave the
	// monitor permanently tinted).
	if mon := glfw.GetPrimaryMonitor(); mon != nil {
		a.gammaTarget = mon
		a.gammaSaved = mon.GetGammaRamp()
	}
	defer a.restoreGamma()

	// Wire callbacks.
	win.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		a.mouseX, a.mouseY = x, y
		a.lastInput = time.Now()
		a.handleHover()
		if a.dragSL != nil {
			a.dragSlider()
		}
	})
	win.SetMouseButtonCallback(func(_ *glfw.Window, btn glfw.MouseButton, action glfw.Action, _ glfw.ModifierKey) {
		if btn != glfw.MouseButtonLeft {
			return
		}
		a.lastInput = time.Now()
		if action == glfw.Press {
			a.mouseLB = true
			a.handleClick()
		} else if action == glfw.Release {
			a.mouseLB = false
			a.dragSL = nil
		}
	})
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		a.lastInput = time.Now()
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

	// Pump PostEmptyEvent so the status footer updates and any pending
	// goroutine work has a chance to flush — without this WaitEvents
	// would block indefinitely while idle.
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				glfw.PostEmptyEvent()
			}
		}
	}()
	defer close(stop)

	// Main loop — WaitEvents-driven.
	for !win.ShouldClose() {
		gl.ClearColor(0.06, 0.07, 0.10, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)
		a.draw(r)
		win.SwapBuffers()
		a.frames++
		// Block until the next event arrives (mouse / key / focus / our
		// own PostEmptyEvent ticker).  Zero CPU when idle.
		glfw.WaitEvents()
	}
}

// ─── Widget construction ────────────────────────────────────────────────────

func (a *app) buildWidgets() {
	const (
		x0     = 20
		labelH = 16
	)
	y := float32(60)

	// Window state row.
	bw, bh, gap := float32(140), float32(34), float32(10)
	mkBtn := func(x, y float32, label string, fn func()) {
		a.buttons = append(a.buttons, &button{
			rect: rect{x: x, y: y, w: bw, h: bh}, label: label, action: fn,
		})
	}
	x := float32(x0)
	mkBtn(x, y, "ICONIFY", func() { a.win.Iconify() })
	x += bw + gap
	mkBtn(x, y, "MAXIMIZE", func() { a.win.Maximize() })
	x += bw + gap
	mkBtn(x, y, "RESTORE", func() { a.win.Restore() })
	x += bw + gap
	mkBtn(x, y, "ATTENTION", func() { a.win.RequestAttention() })

	y += bh + 36

	// Opacity slider.
	a.sliders = append(a.sliders, &slider{
		rect:   rect{x: x0, y: y + labelH + 4, w: 460, h: 20},
		label:  "OPACITY",
		value:  1.0, min: 0.25, max: 1.0,
		onLive: func(v float64) { a.win.SetOpacity(float32(v)) },
	})
	y += labelH + 4 + 24 + 24

	// Title row — single button that randomises the title.
	a.buttons = append(a.buttons, &button{
		rect:    rect{x: x0, y: y, w: 220, h: bh},
		label:   "RANDOMIZE TITLE",
		action:  func() { a.win.SetTitle(randomTitle()) },
		primary: true,
	})
	y += bh + 36

	// Gamma slider.
	a.sliders = append(a.sliders, &slider{
		rect:   rect{x: x0, y: y + labelH + 4, w: 460, h: 20},
		label:  "GAMMA",
		value:  1.0, min: 0.5, max: 2.0,
		onLive: func(v float64) { a.applyGamma(v) },
	})
	y += labelH + 4 + 24 + 24

	// Cursor strip — 6 hover regions + 1 region that uses our custom cursor.
	cw, ch := float32(100), float32(54)
	cgap := float32(6)
	cx := float32(x0)
	mkCursor := func(label string, c *glfw.Cursor) {
		a.hoverRegions = append(a.hoverRegions, &hoverRegion{
			rect:   rect{x: cx, y: y + labelH + 4, w: cw, h: ch},
			label:  label, cursor: c,
		})
		cx += cw + cgap
	}
	// Filled in once cursors are created; placeholders here.
	a.hoverID = -1
	_ = labelH
	// We populate the cursor regions in makeCursors() since they need the
	// concrete *glfw.Cursor objects.  Track the y so makeCursors can use it.
	a.cursorRowY = y
	mkCursor("ARROW", nil) // re-pointed in makeCursors
	mkCursor("IBEAM", nil)
	mkCursor("CROSS", nil)
	mkCursor("HAND", nil)
	mkCursor("HSIZE", nil)
	mkCursor("VSIZE", nil)
	mkCursor("CUSTOM", nil)
}

// ─── Cursors ────────────────────────────────────────────────────────────────

func (a *app) makeCursors() {
	a.standardCursors = map[string]*glfw.Cursor{
		"ARROW": glfw.CreateStandardCursor(glfw.ArrowCursor),
		"IBEAM": glfw.CreateStandardCursor(glfw.IBeamCursor),
		"CROSS": glfw.CreateStandardCursor(glfw.CrosshairCursor),
		"HAND":  glfw.CreateStandardCursor(glfw.HandCursor),
		"HSIZE": glfw.CreateStandardCursor(glfw.HResizeCursor),
		"VSIZE": glfw.CreateStandardCursor(glfw.VResizeCursor),
	}
	a.customCursor = glfw.CreateCursor(makeCursorImage(), 12, 12)

	// Wire each hover region to its cursor object.
	for _, hr := range a.hoverRegions {
		switch hr.label {
		case "CUSTOM":
			hr.cursor = a.customCursor
		default:
			hr.cursor = a.standardCursors[hr.label]
		}
	}
}

func (a *app) releaseCursors() {
	for _, c := range a.standardCursors {
		c.Destroy()
	}
	if a.customCursor != nil {
		a.customCursor.Destroy()
	}
}

// makeCursorImage paints a 24×24 RGBA bullseye-with-crosshair as a custom
// cursor sprite.  The hot-spot is the centre.
func makeCursorImage() image.Image {
	const n = 24
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	cx, cy := float64(n)/2-0.5, float64(n)/2-0.5
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			d := math.Sqrt(dx*dx + dy*dy)
			ring1 := math.Abs(d-9) < 1.2
			ring2 := math.Abs(d-3) < 0.8
			arms := (math.Abs(dx) < 0.7 && d < 11) || (math.Abs(dy) < 0.7 && d < 11)
			var c color.RGBA
			switch {
			case ring2 || (arms && d < 4):
				c = color.RGBA{255, 220, 60, 255}
			case ring1:
				c = color.RGBA{255, 255, 255, 255}
			case arms:
				c = color.RGBA{200, 200, 230, 255}
			default:
				c = color.RGBA{0, 0, 0, 0}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// ─── Hover / click / drag dispatch ──────────────────────────────────────────

func (a *app) handleHover() {
	hover := -1
	for i, hr := range a.hoverRegions {
		if hr.contains(a.mouseX, a.mouseY) {
			hover = i
			break
		}
	}
	if hover != a.hoverID {
		a.hoverID = hover
		var target *glfw.Cursor
		if hover >= 0 {
			target = a.hoverRegions[hover].cursor
		}
		// SetCursor(nil) restores the default arrow.
		a.win.SetCursor(target)
		a.currentCursor = target
		// mark which region is "active" for visual highlight
		for i, hr := range a.hoverRegions {
			hr.active = (i == hover)
		}
	}
}

func (a *app) handleClick() {
	for _, b := range a.buttons {
		if b.contains(a.mouseX, a.mouseY) {
			b.action()
			return
		}
	}
	for _, s := range a.sliders {
		if s.contains(a.mouseX, a.mouseY) {
			a.dragSL = s
			a.dragSlider()
			return
		}
	}
}

func (a *app) dragSlider() {
	if a.dragSL == nil {
		return
	}
	s := a.dragSL
	t := (float32(a.mouseX) - s.x) / s.w
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	v := float64(t)*(s.max-s.min) + s.min
	if v != s.value {
		s.value = v
		if s.onLive != nil {
			s.onLive(v)
		}
	}
}

// ─── Gamma ───────────────────────────────────────────────────────────────────

func (a *app) applyGamma(g float64) {
	if a.gammaTarget == nil {
		return
	}
	// Build a 256-entry power-law ramp.  glfw expects three uint16 slices.
	r := make([]uint16, 256)
	gg := make([]uint16, 256)
	bb := make([]uint16, 256)
	inv := 1.0 / g
	for i := 0; i < 256; i++ {
		v := math.Pow(float64(i)/255, inv)
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		u := uint16(v*65535 + 0.5)
		r[i] = u
		gg[i] = u
		bb[i] = u
	}
	a.gammaTarget.SetGammaRamp(&glfw.GammaRamp{Red: r, Green: gg, Blue: bb})
}

func (a *app) restoreGamma() {
	if a.gammaTarget != nil && a.gammaSaved != nil {
		a.gammaTarget.SetGammaRamp(a.gammaSaved)
	}
}

// ─── Draw ────────────────────────────────────────────────────────────────────

func (a *app) draw(r *render.Renderer) {
	// Header.
	r.Rect(0, 0, winW, 40, 0.10, 0.13, 0.18)
	r.Text(20, 12, 14, 18, 0, "GLFW DASHBOARD", 0.8, 0.9, 1)

	// Section labels.
	r.Text(20, 46, 11, 14, 0, "WINDOW STATE", 0.6, 0.7, 0.85)

	for _, b := range a.buttons {
		hover := b.contains(a.mouseX, a.mouseY)
		col := [3]float32{0.18, 0.22, 0.30}
		if b.primary {
			col = [3]float32{0.20, 0.35, 0.50}
		}
		if hover {
			col[0] += 0.07
			col[1] += 0.07
			col[2] += 0.07
		}
		r.Rect(b.x, b.y, b.w, b.h, col[0], col[1], col[2])
		// Centre the label.
		const lw float32 = 9
		const lh float32 = 13
		tw := render.TextWidth(b.label, lw)
		r.Text(b.x+(b.w-tw)/2, b.y+(b.h-lh)/2, lw, lh, 0, b.label, 0.85, 0.9, 1)
	}

	// Sliders.
	for _, s := range a.sliders {
		r.Text(s.x, s.y-18, 11, 14, 0, s.label, 0.6, 0.7, 0.85)
		// track
		r.Rect(s.x, s.y+s.h/2-2, s.w, 4, 0.20, 0.22, 0.30)
		// handle
		t := (s.value - s.min) / (s.max - s.min)
		hx := s.x + float32(t)*s.w - 8
		r.Rect(hx, s.y, 16, s.h, 0.85, 0.85, 0.95)
		// value text
		val := fmt.Sprintf("%.2F", s.value)
		r.Text(s.x+s.w+12, s.y+s.h/2-7, 10, 14, 0, val, 0.85, 0.9, 1)
	}

	// Cursor strip label + regions.
	r.Text(20, a.cursorRowY, 11, 14, 0, "CURSOR (HOVER)", 0.6, 0.7, 0.85)
	for _, hr := range a.hoverRegions {
		col := [3]float32{0.16, 0.18, 0.24}
		if hr.contains(a.mouseX, a.mouseY) {
			col[0] += 0.10
			col[1] += 0.10
			col[2] += 0.10
		}
		r.Rect(hr.x, hr.y, hr.w, hr.h, col[0], col[1], col[2])
		const lw, lh float32 = 9, 13
		tw := render.TextWidth(hr.label, lw)
		r.Text(hr.x+(hr.w-tw)/2, hr.y+(hr.h-lh)/2, lw, lh, 0, hr.label, 0.85, 0.9, 1)
	}

	// Status footer.
	footY := float32(winH - 50)
	r.Rect(0, footY, winW, winH-footY, 0.04, 0.05, 0.07)
	r.Text(20, footY+8, 9, 12, 0, "EVENT LOOP  WAITEVENTS LOWPOWER", 0.55, 0.65, 0.80)
	idleMS := time.Since(a.lastInput).Milliseconds()
	stat := fmt.Sprintf("FRAMES %d  IDLE %d MS", a.frames, idleMS)
	r.Text(20, footY+26, 9, 12, 0, stat, 0.7, 0.75, 0.85)
}

// ─── Misc ────────────────────────────────────────────────────────────────────

var titleAdjectives = []string{"BUSY", "QUIET", "BRIGHT", "DARK", "FAST", "SLOW", "CALM", "BOLD"}
var titleNouns = []string{"DASHBOARD", "PANEL", "CONSOLE", "MONITOR", "BOARD", "STATION"}

var titleRng = rand.New(rand.NewSource(time.Now().UnixNano()))

func randomTitle() string {
	a := titleAdjectives[titleRng.Intn(len(titleAdjectives))]
	n := titleNouns[titleRng.Intn(len(titleNouns))]
	return fmt.Sprintf("%s %s %d", a, n, titleRng.Intn(1000))
}
