// Conway's Game of Life — fifth entry in the suite.
//
// First program in the suite to use a GPU texture (one byte per cell, with
// nearest-neighbour sampling) instead of one rect per drawn thing.  That
// shaves the draw cost down to a single fullscreen quad, and exercises the
// renderer's brand-new Texture path.
//
// Rules (B3/S23):
//   - A live cell with 2 or 3 live neighbours stays alive.
//   - A dead cell with exactly 3 live neighbours becomes alive.
//   - All other cells die or stay dead.
// The board is toroidal (edges wrap), so gliders can travel forever.
//
// Controls
//   Mouse left-drag   : paint live cells
//   Mouse right-drag  : erase cells
//   Space             : pause / resume
//   N or right arrow  : single-step (when paused)
//   R                 : random fill (~28% density)
//   C                 : clear the board
//   1                 : insert a glider at the cursor
//   2                 : insert a Gosper glider gun at the cursor
//   3                 : insert a pulsar at the cursor
//   + / -             : faster / slower simulation
//   Esc               : quit
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/render"
)

const (
	gridW = 200
	gridH = 150
	cellPx = 4
	winW   = gridW * cellPx
	winH   = gridH*cellPx + hudH
	hudH   = 36

	startTickHz = 12.0
	minTickHz   = 1.0
	maxTickHz   = 120.0
)

func init() { runtime.LockOSThread() }

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

	win, err := glfw.CreateWindow(winW, winH, "Life", nil, nil)
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

	tex := r.NewTexture(gridW, gridH)
	defer tex.Destroy()

	g := newBoard()
	g.tickHz = startTickHz
	g.uploadTo(tex)

	// Edge-triggered keys go through the callback.
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
		switch key {
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		case glfw.KeySpace:
			if action == glfw.Press {
				g.paused = !g.paused
			}
		case glfw.KeyN, glfw.KeyRight:
			g.step()
			g.uploadTo(tex)
		case glfw.KeyR:
			if action == glfw.Press {
				g.randomize()
				g.uploadTo(tex)
			}
		case glfw.KeyC:
			if action == glfw.Press {
				g.clear()
				g.uploadTo(tex)
			}
		case glfw.Key1:
			cx, cy := mouseCell(win)
			g.insert(cx, cy, glider)
			g.uploadTo(tex)
		case glfw.Key2:
			cx, cy := mouseCell(win)
			g.insert(cx, cy, gosperGun)
			g.uploadTo(tex)
		case glfw.Key3:
			cx, cy := mouseCell(win)
			g.insert(cx, cy, pulsar)
			g.uploadTo(tex)
		case glfw.KeyEqual, glfw.KeyKPAdd:
			g.tickHz *= 1.4
			if g.tickHz > maxTickHz {
				g.tickHz = maxTickHz
			}
		case glfw.KeyMinus, glfw.KeyKPSubtract:
			g.tickHz /= 1.4
			if g.tickHz < minTickHz {
				g.tickHz = minTickHz
			}
		}
	})

	last := time.Now()
	tickAccum := 0.0
	for !win.ShouldClose() {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		if dt > 0.1 {
			dt = 0.1
		}
		last = now

		// Painting: while a button is held, set/clear cells under the cursor
		// each frame so a drag actually leaves a trail.
		if win.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press {
			cx, cy := mouseCell(win)
			g.set(cx, cy, true)
			g.uploadTo(tex)
		}
		if win.GetMouseButton(glfw.MouseButtonRight) == glfw.Press {
			cx, cy := mouseCell(win)
			g.set(cx, cy, false)
			g.uploadTo(tex)
		}

		if !g.paused {
			tickAccum += dt
			interval := 1.0 / g.tickHz
			for tickAccum >= interval {
				tickAccum -= interval
				g.step()
			}
			g.uploadTo(tex)
		}

		gl.ClearColor(0.04, 0.05, 0.07, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)

		// Board fills the area below the HUD.
		r.DrawTexture(tex,
			0, hudH, winW, winH-hudH,
			[3]float32{0.45, 0.95, 0.55}, // alive: green
			[3]float32{0.05, 0.06, 0.08}, // dead: near-black
		)

		// HUD.
		r.Rect(0, 0, winW, hudH, 0.08, 0.10, 0.14)
		state := "RUN"
		if g.paused {
			state = "PAUSE"
		}
		r.Text(12, 8, 12, 18, 0, state, 0.85, 0.85, 1)
		genX := float32(120)
		r.Text(genX, 8, 11, 16, 0, "GEN", 0.6, 0.7, 0.9)
		r.Number(genX+render.TextWidth("GEN", 11)+10, 8, 12, 18, 0, g.gen, 1, 1, 1)
		liveX := float32(320)
		r.Text(liveX, 8, 11, 16, 0, "LIVE", 0.6, 0.7, 0.9)
		r.Number(liveX+render.TextWidth("LIVE", 11)+10, 8, 12, 18, 0, g.aliveCount, 1, 1, 1)
		hzX := float32(winW - 200)
		r.Text(hzX, 8, 11, 16, 0, "HZ", 0.6, 0.7, 0.9)
		r.Number(hzX+render.TextWidth("HZ", 11)+10, 8, 12, 18, 0, int(g.tickHz+0.5), 1, 1, 1)

		win.SwapBuffers()
		glfw.PollEvents()
	}
	fmt.Printf("final generation: %d, alive: %d\n", g.gen, g.aliveCount)
}

// ─── Board ───────────────────────────────────────────────────────────────────

type board struct {
	cur, next  []byte // gridW*gridH; values 0 (dead) or 255 (alive)
	gen        int
	aliveCount int
	tickHz     float64
	paused     bool
	rng        *rand.Rand
}

func newBoard() *board {
	b := &board{
		cur:  make([]byte, gridW*gridH),
		next: make([]byte, gridW*gridH),
		rng:  rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	b.randomize()
	return b
}

func (b *board) idx(x, y int) int { return y*gridW + x }

// wrap: toroidal coords.
func wrap(v, max int) int {
	if v < 0 {
		return v + max
	}
	if v >= max {
		return v - max
	}
	return v
}

func (b *board) set(x, y int, alive bool) {
	if x < 0 || x >= gridW || y < 0 || y >= gridH {
		return
	}
	v := byte(0)
	if alive {
		v = 255
	}
	if b.cur[b.idx(x, y)] != v {
		if alive {
			b.aliveCount++
		} else {
			b.aliveCount--
		}
		b.cur[b.idx(x, y)] = v
	}
}

func (b *board) clear() {
	for i := range b.cur {
		b.cur[i] = 0
	}
	b.aliveCount = 0
	b.gen = 0
}

func (b *board) randomize() {
	b.aliveCount = 0
	for i := range b.cur {
		if b.rng.Float64() < 0.28 {
			b.cur[i] = 255
			b.aliveCount++
		} else {
			b.cur[i] = 0
		}
	}
	b.gen = 0
}

// step computes one generation of B3/S23 with toroidal edges.
func (b *board) step() {
	alive := 0
	for y := 0; y < gridH; y++ {
		ym1 := wrap(y-1, gridH) * gridW
		yp1 := wrap(y+1, gridH) * gridW
		yc := y * gridW
		for x := 0; x < gridW; x++ {
			xm1 := wrap(x-1, gridW)
			xp1 := wrap(x+1, gridW)
			n := b.cur[ym1+xm1]&1 + b.cur[ym1+x]&1 + b.cur[ym1+xp1]&1 +
				b.cur[yc+xm1]&1 + b.cur[yc+xp1]&1 +
				b.cur[yp1+xm1]&1 + b.cur[yp1+x]&1 + b.cur[yp1+xp1]&1
			// b.cur stores 0 / 255.  &1 gives the parity bit, which is 1 for
			// 255 and 0 for 0 — so the sum is the live-neighbour count.
			cur := b.cur[yc+x] != 0
			next := byte(0)
			switch {
			case cur && (n == 2 || n == 3):
				next = 255
			case !cur && n == 3:
				next = 255
			}
			b.next[yc+x] = next
			if next != 0 {
				alive++
			}
		}
	}
	b.cur, b.next = b.next, b.cur
	b.aliveCount = alive
	b.gen++
}

func (b *board) uploadTo(t *render.Texture) { t.Upload(b.cur) }

// insert stamps a pattern centred at (cx, cy).  Cells outside the board are
// silently skipped (toroidal wrap would scatter the pattern, which isn't what
// you want from a one-shot stamp).
func (b *board) insert(cx, cy int, p pattern) {
	ox := cx - p.w/2
	oy := cy - p.h/2
	for j := 0; j < p.h; j++ {
		for i := 0; i < p.w; i++ {
			if p.cells[j*p.w+i] == 'X' {
				b.set(ox+i, oy+j, true)
			}
		}
	}
}

// ─── Patterns ────────────────────────────────────────────────────────────────

type pattern struct {
	w, h  int
	cells string // row-major, 'X' = alive, anything else = dead
}

var glider = pattern{
	w: 3, h: 3,
	cells: "" +
		".X." +
		"..X" +
		"XXX",
}

var pulsar = pattern{
	w: 13, h: 13,
	cells: "" +
		"............." +
		"..XXX...XXX.." +
		"............." +
		"X....X.X....X" +
		"X....X.X....X" +
		"X....X.X....X" +
		"..XXX...XXX.." +
		"..XXX...XXX.." +
		"X....X.X....X" +
		"X....X.X....X" +
		"X....X.X....X" +
		"............." +
		"..XXX...XXX..",
}

var gosperGun = pattern{
	w: 36, h: 9,
	cells: "" +
		"........................X..........." +
		"......................X.X..........." +
		"............XX......XX............XX" +
		"...........X...X....XX............XX" +
		"XX........X.....X...XX.............." +
		"XX........X...X.XX....X.X..........." +
		"..........X.....X.......X..........." +
		"...........X...X...................." +
		"............XX......................",
}

// ─── Mouse helpers ───────────────────────────────────────────────────────────

func mouseCell(win *glfw.Window) (int, int) {
	mx, my := win.GetCursorPos()
	cx := int(mx) / cellPx
	cy := (int(my) - hudH) / cellPx
	return cx, cy
}
