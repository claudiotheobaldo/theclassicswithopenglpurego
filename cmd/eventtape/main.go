// Event tape — a diagnostic for glfw-purego.
//
// Wires every GLFW callback we care about and dumps each event as a
// timestamped line on screen, scrolling.  A side panel shows polled state
// (cursor, window, focus, joysticks, monitors) so you can see whether
// callbacks and getters agree.
//
// This isn't a game; it's a five-minute "is the input layer actually wired
// up correctly" tester.  Wiggle the mouse, mash keys, plug a controller in
// and out, drag a file onto the window, alt-tab, resize — every event
// should show up.
//
// Quit with Esc.
package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/render"
)

const (
	winW, winH = 1100, 720

	logW       = 720
	panelX     = logW + 10
	panelW     = winW - panelX - 10
	logRows    = 24
	logRowH    = 20
	logFontW   = 9
	logFontH   = 14
	logGap     = 6
	stripPad   = 12
)

func init() { runtime.LockOSThread() }

// ─── Event log ───────────────────────────────────────────────────────────────

type logEntry struct {
	t    time.Time
	kind string
	text string
}

var (
	logBuf  []logEntry
	logMax  = 256
)

func push(kind, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	logBuf = append(logBuf, logEntry{time.Now(), kind, text})
	if len(logBuf) > logMax {
		logBuf = logBuf[len(logBuf)-logMax:]
	}
}

// ─── Polled state mirrored each frame ────────────────────────────────────────

type liveState struct {
	cursorX, cursorY float64
	winSizeW, winSizeH int
	winPosX, winPosY   int
	fbW, fbH           int
	scaleX, scaleY     float32
	focused            bool
	iconified          bool
	maximized          bool
	hovered            bool

	heldKeys    []string // pretty-printed
	heldButtons []string

	joys     []joyState
	monitors []string
}

type joyState struct {
	id       int
	name     string
	guid     string
	gamepad  bool
	axes     []float32
	buttons  []glfw.Action
	hats     []glfw.JoystickHatState
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
	glfw.WindowHint(glfw.Resizable, glfw.True) // resize is one of the events we want to see

	win, err := glfw.CreateWindow(winW, winH, "Event Tape", nil, nil)
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

	push("BOOT", "EVENT TAPE STARTED")

	// ── Window callbacks ──
	win.SetSizeCallback(func(_ *glfw.Window, w, h int) {
		push("SIZE", "%dx%d", w, h)
	})
	win.SetPosCallback(func(_ *glfw.Window, x, y int) {
		push("POS", "%d,%d", x, y)
	})
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		push("FBSIZE", "%dx%d", w, h)
		gl.Viewport(0, 0, int32(w), int32(h))
	})
	win.SetFocusCallback(func(_ *glfw.Window, focused bool) {
		if focused {
			push("FOCUS", "gained")
		} else {
			push("FOCUS", "lost")
		}
	})
	win.SetIconifyCallback(func(_ *glfw.Window, iconified bool) {
		push("ICON", boolWord(iconified, "iconified", "restored"))
	})
	win.SetMaximizeCallback(func(_ *glfw.Window, maximized bool) {
		push("MAX", boolWord(maximized, "maximized", "restored"))
	})
	win.SetRefreshCallback(func(_ *glfw.Window) {
		push("REFRESH", "")
	})
	win.SetContentScaleCallback(func(_ *glfw.Window, x, y float32) {
		push("SCALE", "%.2f x %.2f", x, y)
	})
	win.SetCloseCallback(func(_ *glfw.Window) {
		push("CLOSE", "requested")
	})

	// ── Input callbacks ──
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		if key == glfw.KeyEscape && action == glfw.Press {
			win.SetShouldClose(true)
		}
		push("KEY", "%-7s %-12s scan=%d mods=%s",
			actionName(action), keyName(key), scancode, modString(mods))
	})
	win.SetCharCallback(func(_ *glfw.Window, char rune) {
		push("CHAR", "U+%04X (%s)", char, runeDisplay(char))
	})
	win.SetMouseButtonCallback(func(_ *glfw.Window, btn glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		push("MOUSE", "%-7s %s mods=%s", actionName(action), mouseName(btn), modString(mods))
	})
	win.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		// don't push every move — just every 16th px to avoid flooding
		if int(x)%16 == 0 || int(y)%16 == 0 {
			push("CURSOR", "%.0f, %.0f", x, y)
		}
	})
	win.SetScrollCallback(func(_ *glfw.Window, dx, dy float64) {
		push("SCROLL", "%+.1f, %+.1f", dx, dy)
	})
	win.SetCursorEnterCallback(func(_ *glfw.Window, entered bool) {
		push("CENTER", boolWord(entered, "entered", "left"))
	})
	win.SetDropCallback(func(_ *glfw.Window, paths []string) {
		push("DROP", "%d path(s)", len(paths))
		for _, p := range paths {
			push("DROP", "  %s", p)
		}
	})
	win.SetCharModsCallback(func(_ *glfw.Window, char rune, mods glfw.ModifierKey) {
		push("CHARMOD", "U+%04X mods=%s", char, modString(mods))
	})

	// ── Peripheral callbacks ──
	glfw.SetJoystickCallback(func(joy glfw.Joystick, event glfw.PeripheralEvent) {
		if event == glfw.Connected {
			push("JOY", "Joystick%d connected (%s)", int(joy)+1, glfw.GetJoystickName(joy))
		} else {
			push("JOY", "Joystick%d disconnected", int(joy)+1)
		}
	})
	glfw.SetMonitorCallback(func(monitor *glfw.Monitor, event glfw.PeripheralEvent) {
		name := "?"
		if monitor != nil {
			name = monitor.GetName()
		}
		push("MON", "%s %s", boolWord(event == glfw.Connected, "+", "-"), name)
	})

	for !win.ShouldClose() {
		state := pollLiveState(win)

		gl.ClearColor(0.04, 0.05, 0.07, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)
		drawTape(r, state)

		win.SwapBuffers()
		glfw.PollEvents()
	}
}

// ─── Per-frame polled state ──────────────────────────────────────────────────

func pollLiveState(win *glfw.Window) liveState {
	var s liveState
	s.cursorX, s.cursorY = win.GetCursorPos()
	s.winSizeW, s.winSizeH = win.GetSize()
	s.winPosX, s.winPosY = win.GetPos()
	s.fbW, s.fbH = win.GetFramebufferSize()
	s.scaleX, s.scaleY = win.GetContentScale()
	s.focused = win.GetAttrib(glfw.Focused) == glfw.True
	s.iconified = win.GetAttrib(glfw.Iconified) == glfw.True
	s.maximized = win.GetAttrib(glfw.Maximized) == glfw.True
	s.hovered = win.GetAttrib(glfw.Hovered) == glfw.True

	for k := glfw.KeySpace; k <= glfw.KeyMenu; k++ {
		if win.GetKey(k) == glfw.Press {
			s.heldKeys = append(s.heldKeys, keyName(k))
			if len(s.heldKeys) >= 6 {
				break
			}
		}
	}
	for b := glfw.MouseButton1; b <= glfw.MouseButton8; b++ {
		if win.GetMouseButton(b) == glfw.Press {
			s.heldButtons = append(s.heldButtons, mouseName(b))
		}
	}

	for j := glfw.Joystick1; j <= glfw.JoystickLast; j++ {
		if !glfw.JoystickPresent(j) {
			continue
		}
		js := joyState{
			id:      int(j) + 1,
			name:    glfw.GetJoystickName(j),
			guid:    glfw.GetJoystickGUID(j),
			gamepad: glfw.JoystickIsGamepad(j),
			axes:    glfw.GetJoystickAxes(j),
			buttons: glfw.GetJoystickButtons(j),
			hats:    glfw.GetJoystickHats(j),
		}
		s.joys = append(s.joys, js)
	}

	for _, m := range glfw.GetMonitors() {
		s.monitors = append(s.monitors, m.GetName())
	}
	return s
}

// ─── Draw ────────────────────────────────────────────────────────────────────

func drawTape(r *render.Renderer, s liveState) {
	// log panel background
	r.Rect(0, 0, logW, winH, 0.06, 0.07, 0.10)
	r.Rect(panelX-2, 0, 2, winH, 0.20, 0.22, 0.30)

	// log header
	r.Text(stripPad, 8, logFontW, logFontH, 0, "EVENT TAPE", 0.9, 0.9, 0.9)

	// log rows, newest at top
	visible := logRows
	if visible > len(logBuf) {
		visible = len(logBuf)
	}
	yStart := float32(36)
	for i := 0; i < visible; i++ {
		e := logBuf[len(logBuf)-1-i]
		y := yStart + float32(i)*logRowH
		alpha := 1.0 - float32(i)/float32(logRows)*0.7
		ts := e.t.Format("15:04:05.000")
		r.Text(stripPad, y, logFontW, logFontH, 0, ts, 0.5*alpha, 0.7*alpha, 0.5*alpha)
		x := stripPad + render.TextWidth(ts, logFontW) + 12
		r.Text(x, y, logFontW, logFontH, 0, render.Sanitize(fmt.Sprintf("%-7s", e.kind)), 0.4*alpha, 0.85*alpha, 1*alpha)
		x += render.TextWidth("XXXXXXX", logFontW) + 12
		// truncate text to fit
		text := render.Sanitize(e.text)
		maxChars := int((float32(logW-stripPad) - x) / (logFontW + 10))
		if len(text) > maxChars {
			text = text[:maxChars]
		}
		r.Text(x, y, logFontW, logFontH, 0, text, alpha, alpha, alpha)
	}

	// live-state panel
	drawPanel(r, &s)
}

func drawPanel(r *render.Renderer, s *liveState) {
	x := float32(panelX + 4)
	y := float32(12)
	const lh = 18
	var fw, fh float32 = 8, 12
	_ = fh

	header := func(t string) {
		r.Text(x, y, fw, fh, 0, render.Sanitize(t), 0.6, 0.9, 0.6)
		y += lh
	}
	line := func(format string, args ...any) {
		text := render.Sanitize(fmt.Sprintf(format, args...))
		max := int(float32(panelW-8) / (fw + 10))
		if len(text) > max {
			text = text[:max]
		}
		r.Text(x, y, fw, fh, 0, text, 0.85, 0.85, 0.9)
		y += lh
	}
	gap := func() { y += lh / 2 }

	header("WINDOW")
	line("SIZE %DX%D", s.winSizeW, s.winSizeH)
	line("POS  %D,%D", s.winPosX, s.winPosY)
	line("FB   %DX%D", s.fbW, s.fbH)
	line("SCAL %.2FX%.2F", s.scaleX, s.scaleY)
	line("FOCUS %s ICON %s", boolWord(s.focused, "Y", "N"), boolWord(s.iconified, "Y", "N"))
	line("MAX %s HOVER %s", boolWord(s.maximized, "Y", "N"), boolWord(s.hovered, "Y", "N"))
	gap()

	header("MOUSE")
	line("X %.0F Y %.0F", s.cursorX, s.cursorY)
	if len(s.heldButtons) == 0 {
		line("HELD -")
	} else {
		line("HELD %s", strings.Join(s.heldButtons, " "))
	}
	gap()

	header("KEYS HELD")
	if len(s.heldKeys) == 0 {
		line("-")
	} else {
		line("%s", strings.Join(s.heldKeys, " "))
	}
	gap()

	header("MONITORS")
	if len(s.monitors) == 0 {
		line("-")
	} else {
		for _, m := range s.monitors {
			line("%s", m)
		}
	}
	gap()

	header("JOYSTICKS")
	if len(s.joys) == 0 {
		line("(NONE)")
	}
	for _, j := range s.joys {
		line("J%d %s", j.id, j.name)
		if j.gamepad {
			line("  GAMEPAD GUID %s", truncate(j.guid, 18))
		}
		if len(j.axes) > 0 {
			parts := make([]string, len(j.axes))
			for i, a := range j.axes {
				parts[i] = fmt.Sprintf("%+.2f", a)
			}
			line("  AX %s", strings.Join(parts, " "))
		}
		if len(j.buttons) > 0 {
			b := strings.Builder{}
			for i, a := range j.buttons {
				if i >= 16 {
					break
				}
				if a == glfw.Press {
					b.WriteString(fmt.Sprintf("%X", i))
				} else {
					b.WriteString("-")
				}
			}
			line("  BTN %s", b.String())
		}
		if len(j.hats) > 0 {
			parts := make([]string, len(j.hats))
			for i, h := range j.hats {
				parts[i] = hatString(h)
			}
			line("  HAT %s", strings.Join(parts, " "))
		}
	}
}

// ─── Pretty-print helpers ────────────────────────────────────────────────────

func boolWord(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

func actionName(a glfw.Action) string {
	switch a {
	case glfw.Press:
		return "PRESS"
	case glfw.Release:
		return "RELEASE"
	case glfw.Repeat:
		return "REPEAT"
	}
	return "?"
}

func modString(m glfw.ModifierKey) string {
	if m == 0 {
		return "-"
	}
	parts := []string{}
	if m&glfw.ModShift != 0 {
		parts = append(parts, "SHIFT")
	}
	if m&glfw.ModControl != 0 {
		parts = append(parts, "CTRL")
	}
	if m&glfw.ModAlt != 0 {
		parts = append(parts, "ALT")
	}
	if m&glfw.ModSuper != 0 {
		parts = append(parts, "SUPER")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "+")
}

func keyName(k glfw.Key) string {
	if name := glfw.GetKeyName(k, 0); name != "" {
		return strings.ToUpper(name)
	}
	switch k {
	case glfw.KeySpace:
		return "SPACE"
	case glfw.KeyEnter:
		return "ENTER"
	case glfw.KeyEscape:
		return "ESC"
	case glfw.KeyTab:
		return "TAB"
	case glfw.KeyBackspace:
		return "BACKSP"
	case glfw.KeyLeft:
		return "LEFT"
	case glfw.KeyRight:
		return "RIGHT"
	case glfw.KeyUp:
		return "UP"
	case glfw.KeyDown:
		return "DOWN"
	case glfw.KeyLeftShift, glfw.KeyRightShift:
		return "SHIFT"
	case glfw.KeyLeftControl, glfw.KeyRightControl:
		return "CTRL"
	case glfw.KeyLeftAlt, glfw.KeyRightAlt:
		return "ALT"
	}
	return fmt.Sprintf("KEY%d", int(k))
}

func mouseName(b glfw.MouseButton) string {
	switch b {
	case glfw.MouseButtonLeft:
		return "LEFT"
	case glfw.MouseButtonRight:
		return "RIGHT"
	case glfw.MouseButtonMiddle:
		return "MIDDLE"
	}
	return fmt.Sprintf("MB%d", int(b))
}

func runeDisplay(r rune) string {
	if r >= 0x20 && r < 0x7F {
		return string(r)
	}
	return "."
}

func hatString(h glfw.JoystickHatState) string {
	switch h {
	case glfw.HatCentered:
		return "C"
	case glfw.HatUp:
		return "U"
	case glfw.HatDown:
		return "D"
	case glfw.HatLeft:
		return "L"
	case glfw.HatRight:
		return "R"
	case glfw.HatRightUp:
		return "RU"
	case glfw.HatRightDown:
		return "RD"
	case glfw.HatLeftUp:
		return "LU"
	case glfw.HatLeftDown:
		return "LD"
	}
	return fmt.Sprintf("%X", int(h))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
