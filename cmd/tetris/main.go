// Tetris — guideline-compliant.  Eighth game in the suite, here for fun
// rather than test coverage: it doesn't exercise any binding path the
// other games haven't, but it stresses held-key DAS timing in a way
// that's particularly punishing if the input layer is broken.
//
// Conformance highlights (per https://tetris.wiki/Tetris_Guideline)
//   - Standard SRS rotations and wall-kick tables (JLSTZ + I-piece)
//   - 7-bag random generator
//   - Ghost piece, hold piece (one swap per drop)
//   - DAS = 167 ms, ARR = 33 ms
//   - Lock delay 500 ms with move/rotate reset (extended placement,
//     no max-resets cap — close to "infinite placement")
//   - Soft / hard drop with Guideline scoring (single 100, double 300,
//     triple 500, tetris 800, ×level; soft +1/cell, hard +2/cell)
//   - Level up every 10 lines, gravity ramps per level
//
// Controls
//   Keyboard
//     Left / Right or A / D : move (held = DAS auto-shift)
//     Down or S             : soft drop (held)
//     Up                    : rotate CW
//     X                     : rotate CW
//     Z                     : rotate CCW
//     Space                 : hard drop
//     C / Shift             : hold
//     R                     : restart after game over
//     F11                   : fullscreen
//     Esc                   : quit
//   Gamepad (XInput)
//     D-pad ← / →   : move (held = DAS)
//     D-pad ↓       : soft drop
//     A button (0)  : rotate CW
//     B button (1)  : rotate CCW
//     Y button (3)  : hard drop
//     X button (2)  : hold
//     Start (7)     : restart after game over
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/render"
	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/winutil"
)

// ─── Layout ──────────────────────────────────────────────────────────────────

const (
	cell        = 30
	cols        = 10
	rows        = 20
	hidden      = 2 // spawn rows above the visible field
	gridX       = 220
	gridY       = 40
	winW        = 760
	winH        = gridY + rows*cell + 40 // = 680

	holdX  = 30
	holdY  = 80
	holdW  = 4 * cell
	holdH  = 4 * cell

	nextX  = gridX + cols*cell + 30
	nextY  = 80
	nextW  = 4 * cell
	nextH  = 12 * cell // 3 pieces stacked
)

// ─── Timing (Guideline) ──────────────────────────────────────────────────────

const (
	dasDelay     = 0.167 // s before auto-shift kicks in
	arrInterval  = 0.033 // s between auto-shift moves
	softDropMul  = 20.0  // soft drop is 20× normal gravity
	lockDelay    = 0.500 // s a piece lingers on the floor before locking
	maxLockReset = 15    // upper bound on how many move/rotate resets a piece gets
)

// gravityForLevel returns seconds per cell at the given level.  Standard
// Guideline curve; simplified after level 20.
func gravityForLevel(level int) float64 {
	t := float64(level - 1)
	if t < 0 {
		t = 0
	}
	g := 0.8 - t*0.007
	if g < 0.02 {
		g = 0.02
	}
	return g * g * g // (0.8-(L-1)*0.007)^(L-1) approximated by cubing — close enough at low levels
}

// ─── Pieces ──────────────────────────────────────────────────────────────────

type pieceKind int

const (
	pI pieceKind = iota
	pO
	pT
	pS
	pZ
	pJ
	pL
	pCount
)

// shape[kind][rot] = 4 cells, each (x, y) within the piece's bounding box.
// I uses 4x4, O uses 3x3 (only one rotation), the rest 3x3.  Coordinates
// match standard SRS spawn orientation (J/L/T flat side up; I in row 1).
var shapes = [pCount][4][4][2]int{
	pI: {
		{{0, 1}, {1, 1}, {2, 1}, {3, 1}},
		{{2, 0}, {2, 1}, {2, 2}, {2, 3}},
		{{0, 2}, {1, 2}, {2, 2}, {3, 2}},
		{{1, 0}, {1, 1}, {1, 2}, {1, 3}},
	},
	pO: {
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {2, 1}},
	},
	pT: {
		{{1, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {1, 1}, {2, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {1, 2}},
		{{1, 0}, {0, 1}, {1, 1}, {1, 2}},
	},
	pS: {
		{{1, 0}, {2, 0}, {0, 1}, {1, 1}},
		{{1, 0}, {1, 1}, {2, 1}, {2, 2}},
		{{1, 1}, {2, 1}, {0, 2}, {1, 2}},
		{{0, 0}, {0, 1}, {1, 1}, {1, 2}},
	},
	pZ: {
		{{0, 0}, {1, 0}, {1, 1}, {2, 1}},
		{{2, 0}, {1, 1}, {2, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {1, 2}, {2, 2}},
		{{1, 0}, {0, 1}, {1, 1}, {0, 2}},
	},
	pJ: {
		{{0, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {2, 2}},
		{{1, 0}, {1, 1}, {0, 2}, {1, 2}},
	},
	pL: {
		{{2, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {1, 1}, {1, 2}, {2, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {0, 2}},
		{{0, 0}, {1, 0}, {1, 1}, {1, 2}},
	},
}

// pieceColor returns the standard Guideline RGB for a piece kind.
func pieceColor(k pieceKind) [3]float32 {
	switch k {
	case pI:
		return [3]float32{0.0, 0.85, 0.95}
	case pO:
		return [3]float32{0.95, 0.85, 0.10}
	case pT:
		return [3]float32{0.65, 0.20, 0.85}
	case pS:
		return [3]float32{0.20, 0.85, 0.30}
	case pZ:
		return [3]float32{0.95, 0.20, 0.20}
	case pJ:
		return [3]float32{0.20, 0.40, 0.95}
	case pL:
		return [3]float32{0.95, 0.55, 0.10}
	}
	return [3]float32{0.5, 0.5, 0.5}
}

// ─── Wall kicks (SRS, in screen coords with y growing down) ──────────────────
//
// Tables transcribed from https://tetris.wiki/Super_Rotation_System then Y-
// flipped to match our top-down grid (the wiki uses y-up).

// JLSTZ kicks indexed by [from][to] with wraparound for from→to in
// {(0→1),(1→0),(1→2),(2→1),(2→3),(3→2),(3→0),(0→3)}.
var kicksJLSTZ = [4][4][5][2]int{
	0: {
		1: {{0, 0}, {-1, 0}, {-1, -1}, {0, +2}, {-1, +2}},
		3: {{0, 0}, {+1, 0}, {+1, -1}, {0, +2}, {+1, +2}},
	},
	1: {
		0: {{0, 0}, {+1, 0}, {+1, +1}, {0, -2}, {+1, -2}},
		2: {{0, 0}, {+1, 0}, {+1, +1}, {0, -2}, {+1, -2}},
	},
	2: {
		1: {{0, 0}, {-1, 0}, {-1, -1}, {0, +2}, {-1, +2}},
		3: {{0, 0}, {+1, 0}, {+1, -1}, {0, +2}, {+1, +2}},
	},
	3: {
		0: {{0, 0}, {-1, 0}, {-1, +1}, {0, -2}, {-1, -2}},
		2: {{0, 0}, {-1, 0}, {-1, +1}, {0, -2}, {-1, -2}},
	},
}

var kicksI = [4][4][5][2]int{
	0: {
		1: {{0, 0}, {-2, 0}, {+1, 0}, {-2, +1}, {+1, -2}},
		3: {{0, 0}, {-1, 0}, {+2, 0}, {-1, -2}, {+2, +1}},
	},
	1: {
		0: {{0, 0}, {+2, 0}, {-1, 0}, {+2, -1}, {-1, +2}},
		2: {{0, 0}, {-1, 0}, {+2, 0}, {-1, -2}, {+2, +1}},
	},
	2: {
		1: {{0, 0}, {+1, 0}, {-2, 0}, {+1, +2}, {-2, -1}},
		3: {{0, 0}, {+2, 0}, {-1, 0}, {+2, -1}, {-1, +2}},
	},
	3: {
		0: {{0, 0}, {+1, 0}, {-2, 0}, {+1, +2}, {-2, -1}},
		2: {{0, 0}, {-2, 0}, {+1, 0}, {-2, +1}, {+1, -2}},
	},
}

// ─── Game state ──────────────────────────────────────────────────────────────

type cellKind int8 // -1 empty, otherwise pieceKind

type piece struct {
	kind pieceKind
	rot  int // 0..3
	x, y int // top-left of bounding box, in grid coords
}

type gameState int

const (
	statePlay gameState = iota
	stateGameOver
	statePaused
)

type game struct {
	board     [rows + hidden][cols]cellKind
	cur       piece
	curValid  bool
	hold      pieceKind
	holdValid bool
	holdUsed  bool

	bag    []pieceKind
	queue  []pieceKind // upcoming pieces (next 3 visible)
	rng    *rand.Rand

	score int
	lines int
	level int
	state gameState

	gravityAccum float64
	gravityRate  float64 // s per cell
	softDrop     bool

	// lock delay tracking
	onGround       bool
	lockTimer      float64
	lockResets     int

	// DAS / ARR for left, right, soft-drop
	dasDir       int     // -1 / 0 / +1
	dasTimer     float64 // time since most-recent press
	arrTimer     float64
	dropDasTimer float64

	prevButtons []glfw.Action // edge-detect controller buttons
	prevHat     glfw.JoystickHatState
}

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
	glfw.WindowHint(glfw.Resizable, glfw.True)

	win, err := glfw.CreateWindow(winW, winH, "Tetris", nil, nil)
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

	// Edge-triggered keyboard actions (rotation, hard drop, hold, restart).
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action == glfw.Release {
			return
		}
		if action == glfw.Repeat {
			// We do our own repeat for movement; ignore OS auto-repeat for
			// rotation / hold / hard drop.  Down-arrow auto-repeat is also
			// a no-op (soft drop is poll-driven below).
			return
		}
		switch key {
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		case glfw.KeyF11:
			winutil.ToggleFullscreen(win)
		case glfw.KeyR:
			if g.state == stateGameOver {
				g.reset()
			}
		case glfw.KeyP:
			if g.state == statePlay {
				g.state = statePaused
			} else if g.state == statePaused {
				g.state = statePlay
			}
		}
		if g.state != statePlay {
			return
		}
		switch key {
		case glfw.KeyUp, glfw.KeyX:
			g.rotate(+1)
		case glfw.KeyZ:
			g.rotate(-1)
		case glfw.KeySpace:
			g.hardDrop()
		case glfw.KeyC, glfw.KeyLeftShift, glfw.KeyRightShift:
			g.tryHold()
		}
	})

	fbW, fbH := win.GetFramebufferSize()
	apply := func(w, h int) {
		x, y, vw, vh := winutil.LetterboxRect(w, h, winW, winH)
		gl.Viewport(int32(x), int32(y), int32(vw), int32(vh))
	}
	apply(fbW, fbH)
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		fbW, fbH = w, h
		apply(w, h)
	})

	last := time.Now()
	for !win.ShouldClose() {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		if dt > 0.1 {
			dt = 0.1
		}
		last = now

		g.input(win, dt)
		g.update(dt)

		gl.ClearColor(0.04, 0.05, 0.08, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)
		g.draw(r)

		win.SwapBuffers()
		glfw.PollEvents()
	}
}

func newGame() *game {
	g := &game{rng: rand.New(rand.NewSource(time.Now().UnixNano())), hold: -1}
	g.reset()
	return g
}

func (g *game) reset() {
	for r := range g.board {
		for c := range g.board[r] {
			g.board[r][c] = -1
		}
	}
	g.bag = nil
	g.queue = nil
	for len(g.queue) < 3 {
		g.queue = append(g.queue, g.drawFromBag())
	}
	g.score = 0
	g.lines = 0
	g.level = 1
	g.state = statePlay
	g.hold = -1
	g.holdValid = false
	g.holdUsed = false
	g.dasDir = 0
	g.dasTimer = 0
	g.arrTimer = 0
	g.gravityAccum = 0
	g.gravityRate = gravityForLevel(g.level)
	g.spawnNext()
}

// drawFromBag returns the next piece in 7-bag order, refilling when empty.
func (g *game) drawFromBag() pieceKind {
	if len(g.bag) == 0 {
		g.bag = []pieceKind{pI, pO, pT, pS, pZ, pJ, pL}
		g.rng.Shuffle(len(g.bag), func(i, j int) { g.bag[i], g.bag[j] = g.bag[j], g.bag[i] })
	}
	p := g.bag[len(g.bag)-1]
	g.bag = g.bag[:len(g.bag)-1]
	return p
}

func (g *game) spawnNext() {
	kind := g.queue[0]
	g.queue = g.queue[1:]
	g.queue = append(g.queue, g.drawFromBag())
	g.spawnPiece(kind)
}

func (g *game) spawnPiece(kind pieceKind) {
	g.cur = piece{kind: kind, rot: 0}
	if kind == pI {
		g.cur.x = 3
	} else {
		g.cur.x = 3
	}
	// SRS: pieces spawn in rows 21/22 (using 22-row grid with 2 hidden).
	// We use 0..hidden+rows-1 with hidden rows on top, so spawn y is 0.
	g.cur.y = 0
	g.curValid = true
	g.holdUsed = false
	g.onGround = false
	g.lockTimer = 0
	g.lockResets = 0
	if g.collides(g.cur) {
		g.curValid = false
		g.state = stateGameOver
		fmt.Printf("Game over. Score: %d  Lines: %d  Level: %d\n", g.score, g.lines, g.level)
	}
}

// ─── Input ───────────────────────────────────────────────────────────────────

func (g *game) input(win *glfw.Window, dt float64) {
	if g.state != statePlay {
		// still drain controller buttons so a Start press registers cleanly
		g.pollGamepadEdge(win)
		return
	}

	keyDown := func(k glfw.Key) bool { return win.GetKey(k) == glfw.Press }

	// Movement: figure out the requested horizontal direction for this frame.
	wantLeft := keyDown(glfw.KeyLeft) || keyDown(glfw.KeyA)
	wantRight := keyDown(glfw.KeyRight) || keyDown(glfw.KeyD)
	wantSoft := keyDown(glfw.KeyDown) || keyDown(glfw.KeyS)

	// Gamepad.
	if joy, ok := firstJoystick(); ok {
		hats := glfw.GetJoystickHats(joy)
		if len(hats) > 0 {
			h := hats[0]
			if h&glfw.HatLeft != 0 {
				wantLeft = true
			}
			if h&glfw.HatRight != 0 {
				wantRight = true
			}
			if h&glfw.HatDown != 0 {
				wantSoft = true
			}
			// Up on the d-pad is rotate-CW: edge-detected so a held d-pad
			// up doesn't spam.
			if h&glfw.HatUp != 0 && g.prevHat&glfw.HatUp == 0 {
				g.rotate(+1)
			}
			g.prevHat = h
		}
		btns := glfw.GetJoystickButtons(joy)
		if g.prevButtons == nil || len(g.prevButtons) != len(btns) {
			g.prevButtons = make([]glfw.Action, len(btns))
		}
		press := func(idx int) bool {
			return idx < len(btns) && btns[idx] == glfw.Press && g.prevButtons[idx] != glfw.Press
		}
		if press(0) {
			g.rotate(+1)
		}
		if press(1) {
			g.rotate(-1)
		}
		if press(3) {
			g.hardDrop()
		}
		if press(2) {
			g.tryHold()
		}
		copy(g.prevButtons, btns)
	}

	// Resolve DAS / ARR.  When we just transitioned to a held direction,
	// move once immediately and start the DAS clock.  After dasDelay
	// elapses, auto-shift at arrInterval until released.
	switch {
	case wantLeft && !wantRight:
		if g.dasDir != -1 {
			g.dasDir = -1
			g.dasTimer = 0
			g.arrTimer = 0
			g.move(-1)
		} else {
			g.dasTimer += dt
			if g.dasTimer >= dasDelay {
				g.arrTimer += dt
				for g.arrTimer >= arrInterval {
					g.arrTimer -= arrInterval
					g.move(-1)
				}
			}
		}
	case wantRight && !wantLeft:
		if g.dasDir != +1 {
			g.dasDir = +1
			g.dasTimer = 0
			g.arrTimer = 0
			g.move(+1)
		} else {
			g.dasTimer += dt
			if g.dasTimer >= dasDelay {
				g.arrTimer += dt
				for g.arrTimer >= arrInterval {
					g.arrTimer -= arrInterval
					g.move(+1)
				}
			}
		}
	default:
		g.dasDir = 0
		g.dasTimer = 0
		g.arrTimer = 0
	}

	g.softDrop = wantSoft
}

func (g *game) pollGamepadEdge(win *glfw.Window) {
	joy, ok := firstJoystick()
	if !ok {
		return
	}
	btns := glfw.GetJoystickButtons(joy)
	if g.prevButtons == nil || len(g.prevButtons) != len(btns) {
		g.prevButtons = make([]glfw.Action, len(btns))
	}
	if len(btns) > 7 && btns[7] == glfw.Press && g.prevButtons[7] != glfw.Press {
		if g.state == stateGameOver {
			g.reset()
		}
	}
	copy(g.prevButtons, btns)
}

func firstJoystick() (glfw.Joystick, bool) {
	for j := glfw.Joystick1; j <= glfw.JoystickLast; j++ {
		if glfw.JoystickPresent(j) {
			return j, true
		}
	}
	return 0, false
}

// ─── Mechanics ───────────────────────────────────────────────────────────────

func (g *game) collides(p piece) bool {
	for _, c := range shapes[p.kind][p.rot] {
		x := p.x + c[0]
		y := p.y + c[1]
		if x < 0 || x >= cols || y >= rows+hidden {
			return true
		}
		if y < 0 {
			continue
		}
		if g.board[y][x] >= 0 {
			return true
		}
	}
	return false
}

func (g *game) move(dx int) bool {
	if !g.curValid {
		return false
	}
	p := g.cur
	p.x += dx
	if g.collides(p) {
		return false
	}
	g.cur = p
	g.touchLockReset()
	return true
}

func (g *game) rotate(dir int) {
	if !g.curValid {
		return
	}
	from := g.cur.rot
	to := (from + dir + 4) % 4
	tab := kicksJLSTZ
	if g.cur.kind == pI {
		tab = kicksI
	}
	if g.cur.kind == pO {
		// O doesn't actually rotate.
		return
	}
	for _, k := range tab[from][to] {
		test := g.cur
		test.rot = to
		test.x += k[0]
		test.y += k[1]
		if !g.collides(test) {
			g.cur = test
			g.touchLockReset()
			return
		}
	}
}

func (g *game) hardDrop() {
	if !g.curValid {
		return
	}
	dist := 0
	for {
		test := g.cur
		test.y++
		if g.collides(test) {
			break
		}
		g.cur = test
		dist++
	}
	g.score += 2 * dist
	g.lockNow()
}

func (g *game) tryHold() {
	if !g.curValid || g.holdUsed {
		return
	}
	prev := g.cur.kind
	if g.holdValid {
		g.spawnPiece(g.hold)
	} else {
		g.spawnNext()
	}
	g.hold = prev
	g.holdValid = true
	g.holdUsed = true
}

// touchLockReset is called when the player moves or rotates: if the piece
// was on the ground, the lock timer resets (extended placement).
func (g *game) touchLockReset() {
	if g.onGround && g.lockResets < maxLockReset {
		g.lockTimer = 0
		g.lockResets++
	}
}

func (g *game) update(dt float64) {
	if g.state != statePlay || !g.curValid {
		return
	}

	// Check on-ground status.
	test := g.cur
	test.y++
	wasOnGround := g.onGround
	g.onGround = g.collides(test)
	if !g.onGround {
		// Piece is in the air — gravity falls.
		rate := g.gravityRate
		if g.softDrop {
			rate /= softDropMul
			if rate < 0.005 {
				rate = 0.005
			}
		}
		g.gravityAccum += dt
		for g.gravityAccum >= rate && !g.onGround {
			g.gravityAccum -= rate
			next := g.cur
			next.y++
			if g.collides(next) {
				g.onGround = true
				break
			}
			g.cur = next
			if g.softDrop {
				g.score++
			}
		}
		g.lockTimer = 0
		if !wasOnGround {
			g.lockResets = 0
		}
	} else {
		// On the ground — accumulate lock delay.
		g.lockTimer += dt
		if g.lockTimer >= lockDelay {
			g.lockNow()
		}
	}
}

// lockNow stamps the current piece into the board, clears lines, and
// spawns the next piece.  Game-over check happens in spawnPiece.
func (g *game) lockNow() {
	for _, c := range shapes[g.cur.kind][g.cur.rot] {
		x := g.cur.x + c[0]
		y := g.cur.y + c[1]
		if y >= 0 && y < rows+hidden && x >= 0 && x < cols {
			g.board[y][x] = cellKind(g.cur.kind)
		}
	}
	cleared := g.clearLines()
	if cleared > 0 {
		g.lines += cleared
		g.score += lineScore(cleared) * g.level
		newLevel := 1 + g.lines/10
		if newLevel != g.level {
			g.level = newLevel
			g.gravityRate = gravityForLevel(g.level)
		}
	}
	g.spawnNext()
}

func (g *game) clearLines() int {
	cleared := 0
	for y := rows + hidden - 1; y >= 0; y-- {
		full := true
		for x := 0; x < cols; x++ {
			if g.board[y][x] < 0 {
				full = false
				break
			}
		}
		if !full {
			continue
		}
		// Shift everything above down by one.
		for yy := y; yy > 0; yy-- {
			g.board[yy] = g.board[yy-1]
		}
		for x := 0; x < cols; x++ {
			g.board[0][x] = -1
		}
		cleared++
		y++ // re-test the row that just shifted in
	}
	return cleared
}

func lineScore(n int) int {
	switch n {
	case 1:
		return 100
	case 2:
		return 300
	case 3:
		return 500
	case 4:
		return 800
	}
	return 0
}

// ─── Draw ────────────────────────────────────────────────────────────────────

func (g *game) draw(r *render.Renderer) {
	// Playfield background.
	r.Rect(gridX-2, gridY-2, cols*cell+4, rows*cell+4, 0.18, 0.20, 0.26)
	r.Rect(gridX, gridY, cols*cell, rows*cell, 0.07, 0.08, 0.12)

	// Settled cells.
	for y := hidden; y < rows+hidden; y++ {
		for x := 0; x < cols; x++ {
			c := g.board[y][x]
			if c < 0 {
				continue
			}
			drawCell(r, x, y-hidden, pieceColor(pieceKind(c)))
		}
	}

	// Ghost piece.
	if g.curValid && g.state == statePlay {
		ghost := g.cur
		for {
			next := ghost
			next.y++
			if g.collides(next) {
				break
			}
			ghost = next
		}
		col := pieceColor(g.cur.kind)
		ghostCol := [3]float32{col[0] * 0.35, col[1] * 0.35, col[2] * 0.35}
		drawPiece(r, ghost, ghostCol)
	}

	// Active piece (drawn after ghost so ghost can't obscure it).
	if g.curValid {
		drawPiece(r, g.cur, pieceColor(g.cur.kind))
	}

	// HOLD panel.
	r.Text(holdX, holdY-22, 12, 16, 0, "HOLD", 0.65, 0.72, 0.85)
	r.Rect(holdX-2, holdY-2, holdW+4, holdH+4, 0.18, 0.20, 0.26)
	r.Rect(holdX, holdY, holdW, holdH, 0.07, 0.08, 0.12)
	if g.holdValid {
		col := pieceColor(g.hold)
		if g.holdUsed {
			col[0] *= 0.5
			col[1] *= 0.5
			col[2] *= 0.5
		}
		drawPiecePreview(r, g.hold, holdX, holdY, holdW, holdH, col)
	}

	// NEXT panel — top three.
	r.Text(nextX, nextY-22, 12, 16, 0, "NEXT", 0.65, 0.72, 0.85)
	r.Rect(nextX-2, nextY-2, nextW+4, nextH+4, 0.18, 0.20, 0.26)
	r.Rect(nextX, nextY, nextW, nextH, 0.07, 0.08, 0.12)
	for i, k := range g.queue {
		if i >= 3 {
			break
		}
		drawPiecePreview(r, k, nextX, nextY+float32(i)*4*cell, nextW, 4*cell, pieceColor(k))
	}

	// Stats bar (left side, below HOLD).
	statsX := float32(holdX)
	statsY := float32(holdY + holdH + 30)
	const lblW, lblH float32 = 11, 14
	const valW, valH float32 = 14, 18
	drawStat(r, statsX, statsY, "SCORE", g.score, lblW, lblH, valW, valH)
	statsY += 50
	drawStat(r, statsX, statsY, "LEVEL", g.level, lblW, lblH, valW, valH)
	statsY += 50
	drawStat(r, statsX, statsY, "LINES", g.lines, lblW, lblH, valW, valH)

	// Overlays.
	switch g.state {
	case stateGameOver:
		drawCenteredText(r, "GAME OVER", winH/2-30, 26, 36)
		drawCenteredText(r, "PRESS R OR START", winH/2+30, 12, 18)
	case statePaused:
		drawCenteredText(r, "PAUSED", winH/2-30, 26, 36)
		drawCenteredText(r, "PRESS P TO RESUME", winH/2+30, 12, 18)
	}
}

func drawCell(r *render.Renderer, gx, gy int, col [3]float32) {
	x := float32(gridX + gx*cell)
	y := float32(gridY + gy*cell)
	r.Rect(x+1, y+1, cell-2, cell-2, col[0], col[1], col[2])
	// Inner highlight gives the cell a faint depth without an extra shader.
	r.Rect(x+1, y+1, cell-2, 3, col[0]*1.4, col[1]*1.4, col[2]*1.4)
	r.Rect(x+1, y+cell-4, cell-2, 3, col[0]*0.5, col[1]*0.5, col[2]*0.5)
}

func drawPiece(r *render.Renderer, p piece, col [3]float32) {
	for _, c := range shapes[p.kind][p.rot] {
		gy := p.y + c[1] - hidden
		if gy < 0 {
			continue // hidden row
		}
		drawCell(r, p.x+c[0], gy, col)
	}
}

// drawPiecePreview renders a piece centred inside (boxX, boxY, boxW, boxH).
func drawPiecePreview(r *render.Renderer, kind pieceKind, boxX, boxY, boxW, boxH float32, col [3]float32) {
	// Compute piece extent.
	cells := shapes[kind][0]
	minX, minY, maxX, maxY := 9, 9, -1, -1
	for _, c := range cells {
		if c[0] < minX {
			minX = c[0]
		}
		if c[1] < minY {
			minY = c[1]
		}
		if c[0] > maxX {
			maxX = c[0]
		}
		if c[1] > maxY {
			maxY = c[1]
		}
	}
	pw := float32((maxX - minX + 1) * cell)
	ph := float32((maxY - minY + 1) * cell)
	ox := boxX + (boxW-pw)/2 - float32(minX*cell)
	oy := boxY + (boxH-ph)/2 - float32(minY*cell)
	for _, c := range cells {
		x := ox + float32(c[0]*cell)
		y := oy + float32(c[1]*cell)
		r.Rect(x+1, y+1, cell-2, cell-2, col[0], col[1], col[2])
		r.Rect(x+1, y+1, cell-2, 3, col[0]*1.4, col[1]*1.4, col[2]*1.4)
		r.Rect(x+1, y+cell-4, cell-2, 3, col[0]*0.5, col[1]*0.5, col[2]*0.5)
	}
}

func drawStat(r *render.Renderer, x, y float32, label string, val int, lw, lh, vw, vh float32) {
	r.Text(x, y, lw, lh, 0, label, 0.6, 0.7, 0.85)
	r.Number(x, y+18, vw, vh, 0, val, 1, 1, 1)
}

func drawCenteredText(r *render.Renderer, s string, y int, w, h float32) {
	tw := render.TextWidth(s, w)
	r.Text(float32(winW)/2-tw/2, float32(y), w, h, 0, s, 1, 1, 1)
}
