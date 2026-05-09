// Multi-window paint — sixth program in the suite.
//
// Two windows share a single OpenGL context's resources via the share
// parameter to glfw.CreateWindow.  A single GL texture lives on the GPU
// once; both windows render it.  Painting in either window mutates the
// canvas and the change is visible in the other on the next frame.
//
// First program in the suite to exercise:
//   - More than one glfw.Window in one process
//   - The share *Window parameter of CreateWindow (until now always nil)
//   - Per-window event loop bookkeeping (which window owns the cursor,
//     which window's MakeContextCurrent we're in, etc.)
//   - Clipboard set/get with multi-kilobyte payloads
//
// Controls (in either window)
//   Left-mouse drag  : paint with the current colour
//   Right-mouse drag : erase
//   1..7             : pick brush colour
//   [ / ]            : decrease / increase brush size
//   C                : clear the canvas
//   R                : random splatter
//   Ctrl+C           : copy the canvas to the system clipboard
//                      (RLE-encoded text — paste it into anything)
//   Ctrl+V           : restore a canvas previously copied with Ctrl+C
//   Esc              : quit (closes both windows)
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"strings"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/render"
)

const (
	canvasW = 200
	canvasH = 134
	cellPx  = 4
	winW    = canvasW * cellPx
	winH    = canvasH*cellPx + hudH
	hudH    = 28
)

// 8-colour palette: index 0 is the dead/background colour, 1..7 are paints.
var palette = [8][3]float32{
	{0.05, 0.06, 0.08}, // 0: bg
	{1.00, 1.00, 1.00}, // 1: white
	{0.95, 0.30, 0.30}, // 2: red
	{0.30, 0.85, 0.40}, // 3: green
	{0.30, 0.55, 0.95}, // 4: blue
	{0.95, 0.85, 0.30}, // 5: yellow
	{0.85, 0.40, 0.95}, // 6: purple
	{0.30, 0.85, 0.85}, // 7: cyan
}

func init() { runtime.LockOSThread() }

// ─── Shared canvas ───────────────────────────────────────────────────────────
//
// One byte per cell, value = palette index.  Both windows mutate this and
// upload it to the GPU texture via whichever context is current at the time.

type canvas struct {
	cells [canvasW * canvasH]byte
	dirty bool
	rng   *rand.Rand
}

func (c *canvas) at(x, y int) byte {
	if x < 0 || x >= canvasW || y < 0 || y >= canvasH {
		return 0
	}
	return c.cells[y*canvasW+x]
}

func (c *canvas) set(x, y int, v byte) {
	if x < 0 || x >= canvasW || y < 0 || y >= canvasH {
		return
	}
	if c.cells[y*canvasW+x] != v {
		c.cells[y*canvasW+x] = v
		c.dirty = true
	}
}

// brush stamps a filled disc of radius r at (cx, cy).
func (c *canvas) brush(cx, cy, r int, v byte) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r {
				c.set(cx+dx, cy+dy, v)
			}
		}
	}
}

func (c *canvas) clear() {
	for i := range c.cells {
		c.cells[i] = 0
	}
	c.dirty = true
}

func (c *canvas) randomize() {
	for i := range c.cells {
		if c.rng.Float64() < 0.05 {
			c.cells[i] = byte(1 + c.rng.Intn(7))
		}
	}
	c.dirty = true
}

// encode produces a compact ASCII representation (run-length encoded) suitable
// for pasting into a text clipboard.  Format:
//
//   PAINTV1 <W> <H>
//   <count>:<value>,<count>:<value>,...
//
// Lengths fit easily in clipboard limits (200×134 = 26800 cells; fully
// random data caps at ~80 KB, sparse drawings are a few hundred bytes).
func (c *canvas) encode() string {
	var b strings.Builder
	fmt.Fprintf(&b, "PAINTV1 %d %d\n", canvasW, canvasH)
	if len(c.cells) > 0 {
		cur := c.cells[0]
		count := 1
		flush := func() {
			if count > 1 {
				fmt.Fprintf(&b, "%d:%d,", count, cur)
			} else {
				fmt.Fprintf(&b, "%d,", cur)
			}
		}
		for i := 1; i < len(c.cells); i++ {
			if c.cells[i] == cur {
				count++
				continue
			}
			flush()
			cur = c.cells[i]
			count = 1
		}
		flush()
	}
	return b.String()
}

// decode replaces the canvas with a previously-encoded string.  Mismatched
// dimensions abort silently — paste-from-anything shouldn't crash.
func (c *canvas) decode(s string) bool {
	lines := strings.SplitN(s, "\n", 2)
	if len(lines) < 2 {
		return false
	}
	var w, h int
	if _, err := fmt.Sscanf(lines[0], "PAINTV1 %d %d", &w, &h); err != nil {
		return false
	}
	if w != canvasW || h != canvasH {
		return false
	}
	body := strings.TrimRight(lines[1], ",\n\r ")
	idx := 0
	for _, tok := range strings.Split(body, ",") {
		if tok == "" {
			continue
		}
		count, val := 1, 0
		if i := strings.IndexByte(tok, ':'); i >= 0 {
			cn, err1 := strconv.Atoi(tok[:i])
			vn, err2 := strconv.Atoi(tok[i+1:])
			if err1 != nil || err2 != nil {
				return false
			}
			count, val = cn, vn
		} else {
			vn, err := strconv.Atoi(tok)
			if err != nil {
				return false
			}
			val = vn
		}
		for k := 0; k < count && idx < len(c.cells); k++ {
			c.cells[idx] = byte(val)
			idx++
		}
	}
	c.dirty = true
	return idx == len(c.cells)
}

// ─── Per-window state ────────────────────────────────────────────────────────

type windowState struct {
	w   *glfw.Window
	r   *render.Renderer
	tex *render.Texture // shared via context-sharing; same GPU object
	id  int             // 1 or 2, for HUD labels
}

// brushState lives at the package level so both windows show the same
// brush colour and size — they're a shared concept.
var (
	brush     = struct{ color byte; radius int }{color: 1, radius: 3}
	app       = &canvas{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
)

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

	// Window 1: primary, no share.
	primary, err := glfw.CreateWindow(winW, winH, "Paint #1", nil, nil)
	if err != nil {
		panic(err)
	}
	primary.MakeContextCurrent()
	if err := gl.Init(); err != nil {
		panic(err)
	}

	r1 := render.New()
	defer r1.Destroy()
	tex := r1.NewTexture(canvasW, canvasH)
	defer tex.Destroy()
	tex.Upload(app.cells[:]) // initial all-zero state

	// Window 2: shares context resources with primary.  The texture above
	// is therefore visible from this context too.
	secondary, err := glfw.CreateWindow(winW, winH, "Paint #2", nil, primary)
	if err != nil {
		panic(err)
	}
	secondary.MakeContextCurrent()
	r2 := render.New()
	defer r2.Destroy()

	w1 := &windowState{w: primary, r: r1, tex: tex, id: 1}
	w2 := &windowState{w: secondary, r: r2, tex: tex, id: 2}

	for _, ws := range []*windowState{w1, w2} {
		wireCallbacks(ws)
	}

	// Sync vsync once on the first window only.  GLFW's swap interval is
	// per-context; setting on the primary is enough for that one's swaps,
	// and we'll set on the secondary when we make its context current.
	primary.MakeContextCurrent()
	glfw.SwapInterval(1)
	secondary.MakeContextCurrent()
	glfw.SwapInterval(1)

	for !primary.ShouldClose() && !secondary.ShouldClose() {
		// Paint while a button is held.
		paintHeld(w1)
		paintHeld(w2)

		// Upload once per frame if anything changed.  Either context can
		// do the upload because they share the texture.
		if app.dirty {
			primary.MakeContextCurrent()
			tex.Upload(app.cells[:])
			app.dirty = false
		}

		// Render each window.
		drawWindow(w1)
		drawWindow(w2)

		glfw.PollEvents()
	}
}

// ─── Per-window plumbing ────────────────────────────────────────────────────

func wireCallbacks(ws *windowState) {
	ws.w.SetKeyCallback(func(win *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
		switch key {
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		case glfw.Key1, glfw.Key2, glfw.Key3, glfw.Key4, glfw.Key5, glfw.Key6, glfw.Key7:
			brush.color = byte(int(key) - int(glfw.Key0))
		case glfw.KeyLeftBracket:
			if brush.radius > 1 {
				brush.radius--
			}
		case glfw.KeyRightBracket:
			if brush.radius < 24 {
				brush.radius++
			}
		case glfw.KeyC:
			if mods&glfw.ModControl != 0 {
				glfw.SetClipboardString(app.encode())
			} else if action == glfw.Press {
				app.clear()
			}
		case glfw.KeyV:
			if mods&glfw.ModControl != 0 {
				if !app.decode(glfw.GetClipboardString()) {
					fmt.Println("clipboard decode failed (not a PAINTV1 string)")
				}
			}
		case glfw.KeyR:
			if action == glfw.Press {
				app.randomize()
			}
		}
	})
}

func paintHeld(ws *windowState) {
	if ws.w.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press {
		x, y, ok := mouseCell(ws.w)
		if ok {
			app.brush(x, y, brush.radius, brush.color)
		}
	}
	if ws.w.GetMouseButton(glfw.MouseButtonRight) == glfw.Press {
		x, y, ok := mouseCell(ws.w)
		if ok {
			app.brush(x, y, brush.radius, 0)
		}
	}
}

func mouseCell(w *glfw.Window) (int, int, bool) {
	mx, my := w.GetCursorPos()
	x := int(mx) / cellPx
	y := (int(my) - hudH) / cellPx
	if y < 0 {
		return 0, 0, false
	}
	return x, y, true
}

func drawWindow(ws *windowState) {
	ws.w.MakeContextCurrent()
	gl.ClearColor(0.04, 0.05, 0.07, 1)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	ws.r.Begin(winW, winH)

	// Render the canvas, mixing background palette[0] with the brush colour.
	// We don't have a palette shader yet, so render once per non-bg colour
	// using a copy-mask approach… that'd be expensive.  Instead, just show
	// the raw byte values mixed against bg using the existing fg/bg shader,
	// keyed on the *current* brush colour so painting feels right.  All
	// non-zero cells render in fg regardless of their stored colour.
	//
	// (Trade-off acknowledged: the 8-colour palette is a fiction in this
	// version; the goal here is exercising context sharing, not building a
	// palette renderer.  Future work: add DrawTextureIndexed.)
	ws.r.DrawTexture(ws.tex,
		0, hudH, winW, winH-hudH,
		palette[brush.color],
		palette[0],
	)

	// HUD.
	ws.r.Rect(0, 0, winW, hudH, 0.10, 0.12, 0.16)
	ws.r.Text(12, 6, 11, 16, 0,
		fmt.Sprintf("PAINT %d", ws.id), 0.8, 0.9, 1)
	// Brush colour swatch.
	ws.r.Rect(140, 6, 16, 16, palette[brush.color][0], palette[brush.color][1], palette[brush.color][2])
	ws.r.Text(166, 6, 11, 16, 0, fmt.Sprintf("RAD %d", brush.radius), 0.8, 0.8, 0.9)
	hint := "L PAINT  R ERASE  CTRL C COPY  CTRL V PASTE"
	ws.r.Text(winW-render.TextWidth(hint, 7)-12, 8, 7, 12, 0, hint, 0.55, 0.6, 0.7)

	ws.w.SwapBuffers()
}
