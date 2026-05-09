// Breakout — fourth classic in the suite.
//
// Per the original 1976 Atari rules:
//   - Eight rows of bricks: bottom-up yellow, yellow, green, green, orange,
//     orange, red, red — 14 bricks per row.
//   - Yellow = 1, green = 3, orange = 5, red = 7 points.
//   - Ball speeds up after 4 hits, again after 12, and any time you break
//     into the orange or red rows.
//   - Paddle halves in width the first time the ball touches the ceiling.
//   - Three lives, clear two boards to "win".
//
// Controls
//   Keyboard
//     Mouse X         : paddle (primary)
//     A/D, Left/Right : paddle (alt)
//     Space           : launch the ball
//     R               : restart after game-over
//     Esc             : quit
//   Gamepad
//     Left stick X    : paddle
//     A button (0)    : launch
//     Start (7)       : restart after game-over
//
// Mouse-driven paddle exercises GetCursorPos, which Pong / Snake / Asteroids
// didn't touch.
package main

import (
	"fmt"
	"math"
	"runtime"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/render"
)

const (
	winW, winH = 800, 600

	// HUD strip across the top.
	hudH = 36

	// Brick layout.
	cols          = 14
	rows          = 8
	brickW        = 52
	brickH        = 18
	brickGapX     = 4
	brickGapY     = 4
	bricksOriginX = (winW - cols*(brickW+brickGapX) + brickGapX) / 2
	bricksOriginY = hudH + 60

	// Paddle.
	paddleY        = winH - 50
	paddleW        = 110
	paddleHalfW    = 55 // shrinks to this once ball hits the ceiling
	paddleH        = 14
	paddleSpeedKey = 720.0 // px/s for keyboard/gamepad input

	// Ball.
	ballSize        = 10
	ballStartSpeed  = 360.0
	ballSpeed4Hit   = 420.0
	ballSpeed12Hit  = 480.0
	ballSpeedOrange = 540.0
	ballSpeedRed    = 600.0
	ballMaxBounce   = 65 // degrees off vertical when hit at paddle edges

	startLives = 3
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

	win, err := glfw.CreateWindow(winW, winH, "Breakout", nil, nil)
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

	// Edge-triggered keyboard actions go through the callback so the input
	// always lands on exactly one tick, even if the frame is unusually long.
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press {
			return
		}
		switch key {
		case glfw.KeySpace:
			g.tryLaunch()
		case glfw.KeyR:
			if g.state == stateGameOver || g.state == stateWon {
				g.reset()
			}
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		}
	})

	// Mouse click also launches.
	win.SetMouseButtonCallback(func(_ *glfw.Window, btn glfw.MouseButton, action glfw.Action, _ glfw.ModifierKey) {
		if btn == glfw.MouseButtonLeft && action == glfw.Press {
			g.tryLaunch()
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

		g.input(win, dt)
		g.update(dt)

		gl.ClearColor(0, 0, 0, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)
		g.draw(r)

		win.SwapBuffers()
		glfw.PollEvents()
	}
}

// ─── Game state ──────────────────────────────────────────────────────────────

type brick struct {
	row     int  // 0 = bottom yellow, 7 = top red
	alive   bool
	x, y    float32
}

type gameState int

const (
	stateServe gameState = iota // ball stuck to paddle, waiting for launch
	statePlay
	stateGameOver
	stateWon
)

type game struct {
	paddleX        float64 // centre-x
	paddleW        float64
	ballX, ballY   float64
	ballVX, ballVY float64
	ballSpeed      float64
	bricks         []brick
	score          int
	highScore      int
	lives          int
	board          int  // 1 or 2 — clearing board 2 wins the game
	hits           int  // total brick hits this life — drives speed-ups
	hitOrange      bool // one-time speed bumps
	hitRed         bool
	ceilingHit     bool // first ceiling touch shrinks the paddle
	state          gameState

	prevButtons []glfw.Action // edge-detect controller buttons
	mouseInited bool
	prevMouseX  float64
}

func newGame() *game {
	g := &game{}
	g.reset()
	return g
}

func (g *game) reset() {
	g.score = 0
	g.lives = startLives
	g.board = 1
	g.paddleW = paddleW
	g.ceilingHit = false
	g.hits = 0
	g.hitOrange = false
	g.hitRed = false
	g.layoutBricks()
	g.serve()
}

func (g *game) nextBoard() {
	g.board++
	if g.board > 2 {
		g.state = stateWon
		fmt.Printf("You win! Final score: %d\n", g.score)
		return
	}
	g.layoutBricks()
	g.hits = 0
	g.hitOrange = false
	g.hitRed = false
	g.ceilingHit = false
	g.paddleW = paddleW
	g.serve()
}

func (g *game) layoutBricks() {
	g.bricks = g.bricks[:0]
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			g.bricks = append(g.bricks, brick{
				row:   r,
				alive: true,
				x:     float32(bricksOriginX + c*(brickW+brickGapX)),
				y:     float32(bricksOriginY + (rows-1-r)*(brickH+brickGapY)),
			})
		}
	}
}

func (g *game) serve() {
	g.state = stateServe
	g.paddleX = winW / 2
	g.ballX = g.paddleX
	g.ballY = paddleY - ballSize - 1
	g.ballSpeed = ballStartSpeed
	g.ballVX, g.ballVY = 0, 0
}

// ─── Input ───────────────────────────────────────────────────────────────────

func (g *game) input(win *glfw.Window, dt float64) {
	keyDown := func(k glfw.Key) bool { return win.GetKey(k) == glfw.Press }

	// 1. Mouse — only if it actually moved this frame, to play nicely with
	// keyboard/gamepad on the same machine.
	mx, _ := win.GetCursorPos()
	if !g.mouseInited {
		g.prevMouseX = mx
		g.mouseInited = true
	}
	if mx != g.prevMouseX {
		g.paddleX = mx
		g.prevMouseX = mx
	}

	// 2. Keyboard.
	move := 0.0
	if keyDown(glfw.KeyA) || keyDown(glfw.KeyLeft) {
		move -= 1
	}
	if keyDown(glfw.KeyD) || keyDown(glfw.KeyRight) {
		move += 1
	}

	// 3. Gamepad: left stick X for paddle, edge-triggered face buttons.
	if joy, ok := firstJoystick(); ok {
		ax := glfw.GetJoystickAxes(joy)
		if len(ax) > 0 {
			v := float64(ax[0])
			if math.Abs(v) > 0.18 {
				move += v
			}
		}
		btns := glfw.GetJoystickButtons(joy)
		if g.prevButtons == nil || len(g.prevButtons) != len(btns) {
			g.prevButtons = make([]glfw.Action, len(btns))
		}
		fire := len(btns) > 0 && btns[0] == glfw.Press && g.prevButtons[0] != glfw.Press
		start := len(btns) > 7 && btns[7] == glfw.Press && g.prevButtons[7] != glfw.Press
		copy(g.prevButtons, btns)
		if fire {
			g.tryLaunch()
		}
		if start && (g.state == stateGameOver || g.state == stateWon) {
			g.reset()
		}
	}

	if move < -1 {
		move = -1
	}
	if move > 1 {
		move = 1
	}
	if move != 0 {
		g.paddleX += move * paddleSpeedKey * dt
	}

	half := g.paddleW / 2
	if g.paddleX < half {
		g.paddleX = half
	}
	if g.paddleX > winW-half {
		g.paddleX = winW - half
	}
}

func (g *game) tryLaunch() {
	if g.state != stateServe {
		return
	}
	g.state = statePlay
	// gentle upward serve with slight random horizontal lean
	angle := -math.Pi/2 + (0.5-rand01())*math.Pi/4 // ±22.5° off straight up
	g.ballVX = math.Cos(angle) * g.ballSpeed
	g.ballVY = math.Sin(angle) * g.ballSpeed
}

func rand01() float64 { return float64(time.Now().UnixNano()%1000) / 1000.0 }

func firstJoystick() (glfw.Joystick, bool) {
	for j := glfw.Joystick1; j <= glfw.JoystickLast; j++ {
		if glfw.JoystickPresent(j) {
			return j, true
		}
	}
	return 0, false
}

// ─── Update ──────────────────────────────────────────────────────────────────

func (g *game) update(dt float64) {
	switch g.state {
	case stateServe:
		// ball rides the paddle
		g.ballX = g.paddleX
		g.ballY = paddleY - ballSize - 1
		return
	case stateGameOver, stateWon:
		return
	}

	// Substep so a high-speed ball can't tunnel through bricks/paddle.
	steps := 1
	speed := math.Sqrt(g.ballVX*g.ballVX + g.ballVY*g.ballVY)
	if speed*dt > brickH/2 {
		steps = int(speed*dt/(brickH/2)) + 1
	}
	sub := dt / float64(steps)
	for i := 0; i < steps; i++ {
		if g.stepBall(sub) {
			break // life lost, stop substepping
		}
	}
}

// stepBall integrates the ball for sub seconds, resolving collisions.
// Returns true if the life ended this step.
func (g *game) stepBall(sub float64) bool {
	g.ballX += g.ballVX * sub
	g.ballY += g.ballVY * sub

	// walls
	if g.ballX < ballSize/2 {
		g.ballX = ballSize / 2
		g.ballVX = -g.ballVX
	} else if g.ballX > winW-ballSize/2 {
		g.ballX = winW - ballSize/2
		g.ballVX = -g.ballVX
	}
	if g.ballY < hudH+ballSize/2 {
		g.ballY = hudH + ballSize/2
		g.ballVY = -g.ballVY
		if !g.ceilingHit {
			g.ceilingHit = true
			g.paddleW = paddleHalfW
		}
	}

	// floor → lose life
	if g.ballY > winH+ballSize {
		g.lives--
		if g.lives <= 0 {
			g.state = stateGameOver
			if g.score > g.highScore {
				g.highScore = g.score
			}
			fmt.Printf("Game over. Score: %d (high: %d)\n", g.score, g.highScore)
			return true
		}
		g.serve()
		return true
	}

	// paddle
	half := g.paddleW / 2
	if g.ballVY > 0 &&
		g.ballY+ballSize/2 >= paddleY &&
		g.ballY-ballSize/2 <= paddleY+paddleH &&
		g.ballX >= g.paddleX-half-ballSize/2 &&
		g.ballX <= g.paddleX+half+ballSize/2 {
		g.ballY = paddleY - ballSize/2 - 0.01
		// reflect with angle based on contact point, like Pong's paddle
		offset := (g.ballX - g.paddleX) / half // in [-1, 1]
		if offset < -1 {
			offset = -1
		} else if offset > 1 {
			offset = 1
		}
		angle := -math.Pi/2 + offset*float64(ballMaxBounce)*math.Pi/180
		sp := math.Sqrt(g.ballVX*g.ballVX + g.ballVY*g.ballVY)
		g.ballVX = math.Cos(angle) * sp
		g.ballVY = math.Sin(angle) * sp
	}

	// bricks
	for i := range g.bricks {
		b := &g.bricks[i]
		if !b.alive {
			continue
		}
		bx, by, bw, bh := float64(b.x), float64(b.y), float64(brickW), float64(brickH)
		if g.ballX+ballSize/2 < bx || g.ballX-ballSize/2 > bx+bw ||
			g.ballY+ballSize/2 < by || g.ballY-ballSize/2 > by+bh {
			continue
		}
		// Determine which side was hit by comparing penetration depths.
		dxLeft := g.ballX + ballSize/2 - bx
		dxRight := bx + bw - (g.ballX - ballSize/2)
		dyTop := g.ballY + ballSize/2 - by
		dyBottom := by + bh - (g.ballY - ballSize/2)
		minOverlap := math.Min(math.Min(dxLeft, dxRight), math.Min(dyTop, dyBottom))
		switch minOverlap {
		case dxLeft, dxRight:
			g.ballVX = -g.ballVX
		case dyTop, dyBottom:
			g.ballVY = -g.ballVY
		}
		b.alive = false
		g.score += brickPoints(b.row)
		if g.score > g.highScore {
			g.highScore = g.score
		}
		g.hits++
		g.applySpeedups(b.row)
		if g.allBricksClear() {
			g.nextBoard()
		}
		break
	}
	return false
}

func brickPoints(row int) int {
	switch {
	case row >= 6: // red rows (top)
		return 7
	case row >= 4: // orange
		return 5
	case row >= 2: // green
		return 3
	default: // yellow
		return 1
	}
}

func (g *game) applySpeedups(row int) {
	switch g.hits {
	case 4:
		g.ballSpeed = math.Max(g.ballSpeed, ballSpeed4Hit)
	case 12:
		g.ballSpeed = math.Max(g.ballSpeed, ballSpeed12Hit)
	}
	if row >= 4 && row < 6 && !g.hitOrange {
		g.hitOrange = true
		g.ballSpeed = math.Max(g.ballSpeed, ballSpeedOrange)
	}
	if row >= 6 && !g.hitRed {
		g.hitRed = true
		g.ballSpeed = math.Max(g.ballSpeed, ballSpeedRed)
	}
	// Apply current target speed by rescaling velocity.
	cur := math.Sqrt(g.ballVX*g.ballVX + g.ballVY*g.ballVY)
	if cur > 0 && g.ballSpeed > cur {
		s := g.ballSpeed / cur
		g.ballVX *= s
		g.ballVY *= s
	}
}

func (g *game) allBricksClear() bool {
	for _, b := range g.bricks {
		if b.alive {
			return false
		}
	}
	return true
}

// ─── Draw ────────────────────────────────────────────────────────────────────

var rowColors = [rows][3]float32{
	0: {0.95, 0.85, 0.20}, // yellow
	1: {0.95, 0.85, 0.20},
	2: {0.30, 0.85, 0.30}, // green
	3: {0.30, 0.85, 0.30},
	4: {0.95, 0.55, 0.15}, // orange
	5: {0.95, 0.55, 0.15},
	6: {0.90, 0.20, 0.20}, // red
	7: {0.90, 0.20, 0.20},
}

func (g *game) draw(r *render.Renderer) {
	// HUD strip.  Layout: SCORE | BOARD <n> | LIVES <n> with explicit
	// per-element widths from the renderer's TextWidth helper so labels and
	// values don't overlap when string lengths or font sizes change.
	r.Rect(0, 0, winW, hudH, 0.06, 0.06, 0.10)
	const labelW, valueW float32 = 14, 16
	const labelH, valueH float32 = 18, 22
	const pad float32 = 12
	r.Number(20, 8, valueW, valueH, 0, g.score, 1, 1, 1)

	boardLabelW := render.TextWidth("BOARD", labelW)
	boardValueW := render.NumberWidth(g.board, valueW)
	boardX := float32(winW)/2 - (boardLabelW+pad+boardValueW)/2
	r.Text(boardX, 8, labelW, labelH, 0, "BOARD", 0.7, 0.7, 0.7)
	r.Number(boardX+boardLabelW+pad, 8, valueW, valueH, 0, g.board, 1, 1, 1)

	livesValueW := render.NumberWidth(g.lives, valueW)
	livesLabelW := render.TextWidth("LIVES", labelW)
	livesValueX := float32(winW) - 20 - livesValueW
	livesLabelX := livesValueX - pad - livesLabelW
	r.Text(livesLabelX, 8, labelW, labelH, 0, "LIVES", 0.7, 0.7, 0.7)
	r.Number(livesValueX, 8, valueW, valueH, 0, g.lives, 1, 1, 1)

	// Bricks.
	for _, b := range g.bricks {
		if !b.alive {
			continue
		}
		col := rowColors[b.row]
		r.Rect(b.x, b.y, brickW, brickH, col[0], col[1], col[2])
	}

	// Side rails — purely cosmetic, mark the playfield boundary.
	r.Rect(0, hudH, 2, winH-hudH, 0.3, 0.3, 0.35)
	r.Rect(winW-2, hudH, 2, winH-hudH, 0.3, 0.3, 0.35)

	// Paddle.
	half := float32(g.paddleW / 2)
	r.Rect(float32(g.paddleX)-half, paddleY, float32(g.paddleW), paddleH, 0.85, 0.85, 0.95)

	// Ball.
	r.Rect(float32(g.ballX)-ballSize/2, float32(g.ballY)-ballSize/2, ballSize, ballSize, 1, 1, 1)

	// Overlays.
	switch g.state {
	case stateServe:
		drawCenteredText(r, "SPACE OR A TO LAUNCH", winH-90, 14, 18)
	case stateGameOver:
		drawCenteredText(r, "GAME OVER", winH/2-30, 28, 40)
		drawCenteredText(r, "PRESS R OR START", winH/2+30, 14, 20)
	case stateWon:
		drawCenteredText(r, "YOU WIN", winH/2-30, 28, 40)
		drawCenteredText(r, "PRESS R OR START", winH/2+30, 14, 20)
	}
}

func drawCenteredText(r *render.Renderer, s string, y int, w, h float32) {
	const gap = 10
	stride := w + gap
	totalW := float32(len(s))*stride - gap
	r.Text(float32(winW)/2-totalW/2, float32(y), w, h, 0, s, 1, 1, 1)
}
