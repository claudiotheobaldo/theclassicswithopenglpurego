// Snake — second classic in the suite.
//
// A grid-based fixed-tick game that exists, in this codebase, as much to
// exercise the gamepad D-pad / hat path in glfw-purego as it does to be
// playable.  Pong covered axes; this one covers buttons-as-hats.
//
// Rules
//   - 30×22 grid of cells, snake starts length 4 in the centre moving right.
//   - Eat the food, grow by one segment, the tick speeds up slightly.
//   - Hit a wall or yourself → game over.
//
// Controls
//   Arrow keys, WASD, or the gamepad D-pad steer.
//   Direction is buffered so two presses inside a single tick still register
//   in order, which prevents the classic "snake reverses into itself" bug.
//   Space         : start / restart after death
//   Esc           : quit
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
	cell    = 24
	cols    = 30
	rows    = 22
	headerH = 36
	winW    = cols * cell
	winH    = rows*cell + headerH

	startTick = 0.14
	minTick   = 0.04
	speedup   = 0.985 // tick *= speedup per food
	startLen  = 4
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

	win, err := glfw.CreateWindow(winW, winH, "Snake", nil, nil)
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

	g := newGame()

	// Direction-change events are queued from a key callback so quick
	// presses inside one tick don't get lost the way GetKey polling would.
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press {
			return
		}
		switch key {
		case glfw.KeyW, glfw.KeyUp:
			g.queueDir(dirUp)
		case glfw.KeyS, glfw.KeyDown:
			g.queueDir(dirDown)
		case glfw.KeyA, glfw.KeyLeft:
			g.queueDir(dirLeft)
		case glfw.KeyD, glfw.KeyRight:
			g.queueDir(dirRight)
		case glfw.KeySpace:
			if g.state != statePlay {
				g.reset()
			}
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		}
	})

	last := time.Now()
	for !win.ShouldClose() {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		if dt > 0.05 {
			dt = 0.05
		}
		last = now

		g.pollGamepad()
		g.update(dt)

		gl.ClearColor(0.05, 0.07, 0.05, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)
		g.draw(r)

		win.SwapBuffers()
		glfw.PollEvents()
	}
}

// ─── Game state ──────────────────────────────────────────────────────────────

type point struct{ x, y int }

type direction int

const (
	dirUp direction = iota
	dirDown
	dirLeft
	dirRight
)

func (d direction) opposite() direction {
	switch d {
	case dirUp:
		return dirDown
	case dirDown:
		return dirUp
	case dirLeft:
		return dirRight
	case dirRight:
		return dirLeft
	}
	return d
}

func (d direction) delta() (dx, dy int) {
	switch d {
	case dirUp:
		return 0, -1
	case dirDown:
		return 0, 1
	case dirLeft:
		return -1, 0
	case dirRight:
		return 1, 0
	}
	return 0, 0
}

type gameState int

const (
	stateReady gameState = iota
	statePlay
	stateDead
)

type game struct {
	snake        []point   // head at index 0
	dir          direction // direction the snake will commit to next tick
	pendingDirs  []direction
	food         point
	tickInterval float64
	tickTimer    float64
	score        int
	highScore    int
	state        gameState
	rng          *rand.Rand
	prevHat      glfw.JoystickHatState // edge-detect D-pad presses
}

func newGame() *game {
	g := &game{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	g.reset()
	return g
}

func (g *game) reset() {
	g.snake = g.snake[:0]
	startX, startY := cols/2, rows/2
	for i := 0; i < startLen; i++ {
		g.snake = append(g.snake, point{startX - i, startY})
	}
	g.dir = dirRight
	g.pendingDirs = g.pendingDirs[:0]
	g.tickInterval = startTick
	g.tickTimer = 0
	g.score = 0
	g.placeFood()
	g.state = statePlay
}

// queueDir buffers a direction change.  Multiple presses within one tick are
// applied one-per-tick so a fast turn (e.g. up-then-right) doesn't get
// merged into a single 90° turn.
func (g *game) queueDir(d direction) {
	if g.state != statePlay {
		return
	}
	last := g.dir
	if n := len(g.pendingDirs); n > 0 {
		last = g.pendingDirs[n-1]
	}
	if d == last || d == last.opposite() {
		return
	}
	if len(g.pendingDirs) < 2 { // two-deep buffer is plenty
		g.pendingDirs = append(g.pendingDirs, d)
	}
}

// pollGamepad runs every frame and turns hat-state edges into queued direction
// changes.  Edge-detection avoids spamming queueDir on every frame the D-pad
// is held.
func (g *game) pollGamepad() {
	for j := glfw.Joystick1; j <= glfw.JoystickLast; j++ {
		if !glfw.JoystickPresent(j) {
			continue
		}
		hats := glfw.GetJoystickHats(j)
		if len(hats) == 0 {
			return
		}
		hat := hats[0]
		pressed := hat & ^g.prevHat
		switch {
		case pressed&glfw.HatUp != 0:
			g.queueDir(dirUp)
		case pressed&glfw.HatDown != 0:
			g.queueDir(dirDown)
		case pressed&glfw.HatLeft != 0:
			g.queueDir(dirLeft)
		case pressed&glfw.HatRight != 0:
			g.queueDir(dirRight)
		}
		// Start (button 7 on most XInput pads) restarts after death.
		if g.state != statePlay {
			btns := glfw.GetJoystickButtons(j)
			if len(btns) > 7 && btns[7] == glfw.Press {
				g.reset()
			}
		}
		g.prevHat = hat
		return
	}
}

func (g *game) update(dt float64) {
	if g.state != statePlay {
		return
	}
	g.tickTimer += dt
	for g.tickTimer >= g.tickInterval {
		g.tickTimer -= g.tickInterval
		g.step()
		if g.state != statePlay {
			return
		}
	}
}

func (g *game) step() {
	if len(g.pendingDirs) > 0 {
		g.dir = g.pendingDirs[0]
		g.pendingDirs = g.pendingDirs[1:]
	}
	dx, dy := g.dir.delta()
	head := g.snake[0]
	next := point{head.x + dx, head.y + dy}

	// wall collision
	if next.x < 0 || next.x >= cols || next.y < 0 || next.y >= rows {
		g.die()
		return
	}
	// self collision (skip the tail tip — it'll vacate this tick unless we
	// just ate)
	tail := g.snake[len(g.snake)-1]
	for i, s := range g.snake {
		if i == len(g.snake)-1 && next != g.food {
			continue
		}
		if s == next {
			g.die()
			return
		}
	}

	// move
	g.snake = append([]point{next}, g.snake...)
	if next == g.food {
		g.score++
		if g.score > g.highScore {
			g.highScore = g.score
		}
		g.tickInterval *= speedup
		if g.tickInterval < minTick {
			g.tickInterval = minTick
		}
		g.placeFood()
	} else {
		g.snake = g.snake[:len(g.snake)-1]
		_ = tail
	}
}

func (g *game) die() {
	g.state = stateDead
	fmt.Printf("Game over. Score: %d (high: %d)\n", g.score, g.highScore)
}

// placeFood drops food onto a uniformly-random empty cell.  Reservoir-style
// rejection is fine since the board is small.
func (g *game) placeFood() {
	occ := make(map[point]bool, len(g.snake))
	for _, s := range g.snake {
		occ[s] = true
	}
	for {
		p := point{g.rng.Intn(cols), g.rng.Intn(rows)}
		if !occ[p] {
			g.food = p
			return
		}
	}
}

// ─── Draw ────────────────────────────────────────────────────────────────────

func (g *game) draw(r *render.Renderer) {
	// header strip
	r.Rect(0, 0, winW, headerH, 0.10, 0.13, 0.10)
	r.Number(8, 6, 18, 24, 3, g.score, 1, 1, 1)
	if g.highScore > 0 {
		r.Text(winW-200, 6, 14, 18, 2, "HI", 0.7, 0.7, 0.7)
		r.Number(winW-160, 6, 18, 24, 3, g.highScore, 0.9, 0.9, 0.9)
	}

	// playfield background
	r.Rect(0, headerH, winW, winH-headerH, 0.04, 0.06, 0.04)

	// food (pulsing red)
	pulse := 0.85 + 0.15*float32(0.5+0.5*sin(time.Now().UnixNano()))
	r.Rect(
		float32(g.food.x*cell)+2, float32(g.food.y*cell+headerH)+2,
		cell-4, cell-4,
		pulse, 0.2, 0.2,
	)

	// snake body (head brighter than the tail)
	for i, s := range g.snake {
		shade := 1.0 - float32(i)*0.6/float32(len(g.snake))
		if shade < 0.4 {
			shade = 0.4
		}
		r.Rect(
			float32(s.x*cell)+1, float32(s.y*cell+headerH)+1,
			cell-2, cell-2,
			0.3*shade, shade, 0.3*shade,
		)
	}

	if g.state == stateDead {
		// dim overlay (no blending, fake with a dark grey wash on the centre)
		r.Rect(0, winH/2-60, winW, 120, 0, 0, 0)
		msg := "GAME OVER"
		w := float32(len(msg))*30 + float32(len(msg)-1)*10
		r.Text(float32(winW)/2-w/2, winH/2-40, 26, 38, 4, msg, 1, 0.4, 0.4)
		hint := "SPACE"
		hw := float32(len(hint))*18 + float32(len(hint)-1)*10
		r.Text(float32(winW)/2-hw/2, winH/2+10, 16, 22, 3, hint, 0.8, 0.8, 0.8)
	}
}

// sin returns a coarse approximation of sin(t·2π) using only nanosecond
// arithmetic — good enough to wobble food brightness without dragging in
// math just for one call.
func sin(ns int64) float64 {
	const period = 6e8 // 600 ms
	t := float64(ns%int64(period)) / period
	// triangle wave in [-1, 1]
	if t < 0.5 {
		return 4*t - 1
	}
	return 3 - 4*t
}
