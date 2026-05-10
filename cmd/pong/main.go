// Pong — first classic in the suite.
//
// Two paddles, a ball, first to 11 wins.  Paddle hits steepen the ball's angle
// based on the contact point and bump its speed up a notch each rally.
// Ball travels in straight lines, bounces off top and bottom walls.
//
// Controls
//   Left paddle  : W/S, or left thumbstick Y on a connected gamepad
//   Right paddle : Up/Down arrows, right thumbstick Y, or AI when idle
//   Space        : start / serve / restart after match point
//   Esc          : quit
//
// The right paddle defaults to AI; if either thumbstick on the right side of
// the controller is moved, control hands over to the human until the next
// serve.  This makes the game playable solo with one controller and gives the
// gamepad path real exercise.
package main

import (
	"fmt"
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
	winW, winH = 800, 600

	paddleW, paddleH = 12.0, 80.0
	paddleMargin     = 24.0
	paddleSpeed      = 460.0
	aiMaxSpeed       = 380.0

	ballSize       = 12.0
	ballStartSpeed = 360.0
	ballSpeedup    = 1.06
	ballMaxSpeed   = 900.0
	ballMaxBounce  = 60.0

	scoreToWin = 11
	serveDelay = 0.6
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
	glfw.WindowHint(glfw.Resizable, glfw.True)

	win, err := glfw.CreateWindow(winW, winH, "Pong", nil, nil)
	if err != nil {
		panic(err)
	}
	// First consumer of SetSizeLimits + SetAspectRatio: lock the window
	// to its 4:3 design ratio with a sensible minimum, then letterbox
	// the playfield inside whatever the framebuffer becomes.  Without
	// the aspect lock the paddles would smear horizontally on resize.
	win.SetSizeLimits(400, 300, glfw.DontCare, glfw.DontCare)
	win.SetAspectRatio(4, 3)
	win.MakeContextCurrent()
	glfw.SwapInterval(1)

	if err := gl.Init(); err != nil {
		panic(err)
	}

	// Update viewport on resize so the design-space (winW × winH) stays
	// proportional inside whatever framebuffer Win32 hands us.  The
	// aspect lock above keeps the framebuffer 4:3, so this letterbox
	// rect always fills it.
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
	_ = fbW
	_ = fbH

	r := render.New()
	defer r.Destroy()

	g := newGame()

	last := time.Now()
	for !win.ShouldClose() {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		if dt > 0.05 {
			dt = 0.05
		}
		last = now

		g.input(win)
		g.update(dt, win)

		gl.ClearColor(0, 0, 0, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)
		g.draw(r)

		win.SwapBuffers()
		glfw.PollEvents()

		if win.GetKey(glfw.KeyEscape) == glfw.Press {
			win.SetShouldClose(true)
		}
	}
}

// ─── Game state ──────────────────────────────────────────────────────────────

type game struct {
	leftY, rightY         float64
	leftScore, rightScore int
	bx, by, bvx, bvy      float64
	speed                 float64
	state                 gameState
	stateTimer            float64
	rng                   *rand.Rand
	rightHumanActive      bool
}

type gameState int

const (
	stateServe gameState = iota
	statePlay
	stateMatchOver
)

func newGame() *game {
	g := &game{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	g.reset(true)
	return g
}

func (g *game) reset(toStart bool) {
	g.leftY = winH/2 - paddleH/2
	g.rightY = winH/2 - paddleH/2
	g.bx = winW/2 - ballSize/2
	g.by = winH/2 - ballSize/2
	g.speed = ballStartSpeed
	dir := 1.0
	if g.rng.Intn(2) == 0 {
		dir = -1.0
	}
	angle := (g.rng.Float64()*2 - 1) * (math.Pi / 8)
	g.bvx = dir * g.speed * math.Cos(angle)
	g.bvy = g.speed * math.Sin(angle)
	if toStart {
		g.leftScore, g.rightScore = 0, 0
	}
	g.state = stateServe
	g.stateTimer = serveDelay
}

func (g *game) input(win *glfw.Window) {
	if win.GetKey(glfw.KeySpace) == glfw.Press {
		if g.state == stateServe && g.stateTimer <= 0 {
			g.state = statePlay
		} else if g.state == stateMatchOver {
			g.reset(true)
		}
	}
}

func readControllerAxis(idx int) float64 {
	for j := glfw.Joystick1; j <= glfw.JoystickLast; j++ {
		if !glfw.JoystickPresent(j) {
			continue
		}
		ax := glfw.GetJoystickAxes(j)
		if idx < len(ax) {
			v := float64(ax[idx])
			if math.Abs(v) < 0.18 {
				return 0
			}
			return v
		}
		return 0
	}
	return 0
}

func (g *game) update(dt float64, win *glfw.Window) {
	keyDown := func(k glfw.Key) bool { return win.GetKey(k) == glfw.Press }

	switch g.state {
	case stateServe:
		g.stateTimer -= dt
	case stateMatchOver:
		return
	}

	var leftMove float64
	if keyDown(glfw.KeyW) {
		leftMove -= 1
	}
	if keyDown(glfw.KeyS) {
		leftMove += 1
	}
	if ax := readControllerAxis(1); ax != 0 {
		leftMove += ax
	}
	g.leftY += clamp(leftMove, -1, 1) * paddleSpeed * dt

	rax := readControllerAxis(3)
	rUp := keyDown(glfw.KeyUp)
	rDn := keyDown(glfw.KeyDown)
	if rax != 0 || rUp || rDn {
		g.rightHumanActive = true
	}
	if g.state == stateServe {
		g.rightHumanActive = (rax != 0 || rUp || rDn)
	}

	if g.rightHumanActive {
		var rm float64
		if rUp {
			rm -= 1
		}
		if rDn {
			rm += 1
		}
		rm += rax
		g.rightY += clamp(rm, -1, 1) * paddleSpeed * dt
	} else {
		var target float64
		if g.bvx > 0 {
			target = g.by + ballSize/2 - paddleH/2
		} else {
			target = winH/2 - paddleH/2
		}
		dy := target - g.rightY
		step := aiMaxSpeed * dt
		if dy > step {
			dy = step
		} else if dy < -step {
			dy = -step
		}
		g.rightY += dy
	}

	g.leftY = clamp(g.leftY, 0, winH-paddleH)
	g.rightY = clamp(g.rightY, 0, winH-paddleH)

	if g.state != statePlay {
		return
	}
	g.bx += g.bvx * dt
	g.by += g.bvy * dt

	if g.by < 0 {
		g.by = 0
		g.bvy = -g.bvy
	} else if g.by+ballSize > winH {
		g.by = winH - ballSize
		g.bvy = -g.bvy
	}

	if g.bvx < 0 && g.bx <= paddleMargin+paddleW && g.bx+ballSize >= paddleMargin {
		if g.by+ballSize >= g.leftY && g.by <= g.leftY+paddleH {
			g.bx = paddleMargin + paddleW
			g.bounceOffPaddle(g.leftY, +1)
		}
	}
	if g.bvx > 0 && g.bx+ballSize >= winW-paddleMargin-paddleW && g.bx <= winW-paddleMargin {
		if g.by+ballSize >= g.rightY && g.by <= g.rightY+paddleH {
			g.bx = winW - paddleMargin - paddleW - ballSize
			g.bounceOffPaddle(g.rightY, -1)
		}
	}

	if g.bx+ballSize < 0 {
		g.rightScore++
		g.afterPoint()
	} else if g.bx > winW {
		g.leftScore++
		g.afterPoint()
	}
}

func (g *game) bounceOffPaddle(paddleTop, dir float64) {
	contact := ((g.by + ballSize/2) - (paddleTop + paddleH/2)) / (paddleH / 2)
	contact = clamp(contact, -1, 1)
	angle := contact * (ballMaxBounce * math.Pi / 180)
	g.speed *= ballSpeedup
	if g.speed > ballMaxSpeed {
		g.speed = ballMaxSpeed
	}
	g.bvx = dir * g.speed * math.Cos(angle)
	g.bvy = g.speed * math.Sin(angle)
}

func (g *game) afterPoint() {
	if g.leftScore >= scoreToWin || g.rightScore >= scoreToWin {
		g.state = stateMatchOver
		fmt.Printf("Match over: %d - %d\n", g.leftScore, g.rightScore)
		return
	}
	g.reset(false)
}

func (g *game) draw(r *render.Renderer) {
	for y := 0.0; y < winH; y += 24 {
		r.Rect(winW/2-2, float32(y)+4, 4, 14, 0.6, 0.6, 0.6)
	}
	r.Rect(paddleMargin, float32(g.leftY), paddleW, paddleH, 1, 1, 1)
	r.Rect(winW-paddleMargin-paddleW, float32(g.rightY), paddleW, paddleH, 1, 1, 1)
	r.Rect(float32(g.bx), float32(g.by), ballSize, ballSize, 1, 1, 1)

	r.Number(winW/4-30, 40, 40, 60, 6, g.leftScore, 1, 1, 1)
	r.Number(winW*3/4-30, 40, 40, 60, 6, g.rightScore, 1, 1, 1)

	if g.state == stateMatchOver {
		msg := "L WIN"
		if g.rightScore > g.leftScore {
			msg = "R WIN"
		}
		const w, h float32 = 26, 40
		r.Text(winW/2-render.TextWidth(msg, w)/2, winH/2+60, w, h, 4, msg, 1, 1, 1)
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
