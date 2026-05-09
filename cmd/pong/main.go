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
	"strings"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"
)

// ─── Layout (logical units) ──────────────────────────────────────────────────
//
// We render in a fixed 800×600 logical coordinate system mapped to clip space
// in the vertex shader.  Paddle and ball sizes are picked to feel like the
// 1972 cabinet.

const (
	winW, winH = 800, 600

	paddleW, paddleH = 12.0, 80.0
	paddleMargin     = 24.0   // distance from screen edge to paddle face
	paddleSpeed      = 460.0  // logical units per second
	aiMaxSpeed       = 380.0  // AI is intentionally slower than the player

	ballSize        = 12.0
	ballStartSpeed  = 360.0
	ballSpeedup     = 1.06    // multiplier per paddle hit
	ballMaxSpeed    = 900.0
	ballMaxBounce   = 60.0    // max ° off horizontal after a paddle hit

	scoreToWin   = 11
	serveDelay   = 0.6        // seconds between point and next serve
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

	win, err := glfw.CreateWindow(winW, winH, "Pong", nil, nil)
	if err != nil {
		panic(err)
	}
	win.MakeContextCurrent()
	glfw.SwapInterval(1)

	if err := gl.Init(); err != nil {
		panic(err)
	}

	r := newRenderer()
	defer r.destroy()

	g := newGame()

	last := time.Now()
	for !win.ShouldClose() {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		if dt > 0.05 {
			dt = 0.05 // clamp huge frame gaps so physics doesn't tunnel
		}
		last = now

		g.input(win)
		g.update(dt, win)

		gl.ClearColor(0, 0, 0, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		g.draw(r)

		win.SwapBuffers()
		glfw.PollEvents()

		if win.GetKey(glfw.KeyEscape) == glfw.Press {
			win.SetShouldClose(true)
		}
	}
}

// ─── Renderer ────────────────────────────────────────────────────────────────
//
// One VAO holding a unit quad, one shader.  Each draw call sets a
// `rect = (x, y, w, h)` uniform and a colour; the vertex shader maps the unit
// quad into pixel space and then to clip space.

type renderer struct {
	prog      uint32
	vao, vbo  uint32
	uRect     int32
	uColor    int32
	uViewport int32
}

func newRenderer() *renderer {
	r := &renderer{}
	r.prog = compileProgram(vsSrc, fsSrc)
	r.uRect = gl.GetUniformLocation(r.prog, gl.Str("uRect\x00"))
	r.uColor = gl.GetUniformLocation(r.prog, gl.Str("uColor\x00"))
	r.uViewport = gl.GetUniformLocation(r.prog, gl.Str("uViewport\x00"))

	quad := []float32{0, 0, 1, 0, 0, 1, 1, 1}
	gl.GenVertexArrays(1, &r.vao)
	gl.BindVertexArray(r.vao)
	gl.GenBuffers(1, &r.vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(quad)*4, gl.Ptr(quad), gl.STATIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 0, 0)

	return r
}

func (r *renderer) destroy() {
	gl.DeleteBuffers(1, &r.vbo)
	gl.DeleteVertexArrays(1, &r.vao)
	gl.DeleteProgram(r.prog)
}

func (r *renderer) begin() {
	gl.UseProgram(r.prog)
	gl.BindVertexArray(r.vao)
	gl.Uniform2f(r.uViewport, float32(winW), float32(winH))
}

func (r *renderer) rect(x, y, w, h, cr, cg, cb float32) {
	gl.Uniform4f(r.uRect, x, y, w, h)
	gl.Uniform3f(r.uColor, cr, cg, cb)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
}

const vsSrc = `#version 330 core
layout(location=0) in vec2 aQuad;
uniform vec4 uRect;       // x, y, w, h in pixels (origin top-left)
uniform vec2 uViewport;   // window size in pixels
void main() {
    vec2 px = uRect.xy + aQuad * uRect.zw;
    vec2 ndc = vec2(
         px.x / uViewport.x * 2.0 - 1.0,
        -(px.y / uViewport.y * 2.0 - 1.0)
    );
    gl_Position = vec4(ndc, 0.0, 1.0);
}` + "\x00"

const fsSrc = `#version 330 core
uniform vec3 uColor;
out vec4 fragColor;
void main() { fragColor = vec4(uColor, 1.0); }` + "\x00"

func compileProgram(vs, fs string) uint32 {
	v := compileShader(gl.VERTEX_SHADER, vs)
	f := compileShader(gl.FRAGMENT_SHADER, fs)
	p := gl.CreateProgram()
	gl.AttachShader(p, v)
	gl.AttachShader(p, f)
	gl.LinkProgram(p)
	var status int32
	gl.GetProgramiv(p, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var n int32
		gl.GetProgramiv(p, gl.INFO_LOG_LENGTH, &n)
		log := strings.Repeat("\x00", int(n)+1)
		gl.GetProgramInfoLog(p, n, nil, gl.Str(log))
		panic("link: " + log)
	}
	gl.DeleteShader(v)
	gl.DeleteShader(f)
	return p
}

func compileShader(kind uint32, src string) uint32 {
	s := gl.CreateShader(kind)
	cs, free := gl.Strs(src)
	defer free()
	gl.ShaderSource(s, 1, cs, nil)
	gl.CompileShader(s)
	var status int32
	gl.GetShaderiv(s, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var n int32
		gl.GetShaderiv(s, gl.INFO_LOG_LENGTH, &n)
		log := strings.Repeat("\x00", int(n)+1)
		gl.GetShaderInfoLog(s, n, nil, gl.Str(log))
		panic("compile: " + log)
	}
	return s
}

// ─── Game state ──────────────────────────────────────────────────────────────

type game struct {
	leftY, rightY        float64 // top of paddle
	leftScore, rightScore int
	bx, by, bvx, bvy     float64
	speed                float64
	state                gameState
	stateTimer           float64
	rng                  *rand.Rand

	// Carries forward across frames so we know whether the right-side player
	// is currently inputting on the controller; if so, AI stops driving them.
	rightHumanActive bool
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
	// random serve direction, gentle vertical component
	dir := 1.0
	if g.rng.Intn(2) == 0 {
		dir = -1.0
	}
	angle := (g.rng.Float64()*2 - 1) * (math.Pi / 8) // ±22.5°
	g.bvx = dir * g.speed * math.Cos(angle)
	g.bvy = g.speed * math.Sin(angle)
	if toStart {
		g.leftScore, g.rightScore = 0, 0
		g.state = stateServe
	} else {
		g.state = stateServe
		g.stateTimer = serveDelay
	}
}

// ─── Input ───────────────────────────────────────────────────────────────────

func (g *game) input(win *glfw.Window) {
	// Space: serve / restart.
	if win.GetKey(glfw.KeySpace) == glfw.Press {
		if g.state == stateServe && g.stateTimer <= 0 {
			g.state = statePlay
		} else if g.state == stateMatchOver {
			g.reset(true)
			g.state = stateServe
		}
	}
}

// readControllerAxis returns the first connected XInput-style gamepad's axis
// reading at idx, or 0 if no controller is present.
func readControllerAxis(idx int) float64 {
	for j := glfw.Joystick1; j <= glfw.JoystickLast; j++ {
		if !glfw.JoystickPresent(j) {
			continue
		}
		ax := glfw.GetJoystickAxes(j)
		if idx < len(ax) {
			v := float64(ax[idx])
			// deadzone
			if math.Abs(v) < 0.18 {
				return 0
			}
			return v
		}
		return 0
	}
	return 0
}

// ─── Update ──────────────────────────────────────────────────────────────────

func (g *game) update(dt float64, win *glfw.Window) {
	keyDown := func(k glfw.Key) bool { return win.GetKey(k) == glfw.Press }

	switch g.state {
	case stateServe:
		g.stateTimer -= dt
	case stateMatchOver:
		// idle until Space resets
		return
	}

	// ── Left paddle ──
	var leftMove float64
	if keyDown(glfw.KeyW) {
		leftMove -= 1
	}
	if keyDown(glfw.KeyS) {
		leftMove += 1
	}
	if ax := readControllerAxis(1); ax != 0 { // left stick Y, GLFW: down = +1
		leftMove += ax
	}
	g.leftY += clamp(leftMove, -1, 1) * paddleSpeed * dt

	// ── Right paddle: AI by default, controller hands over when active ──
	rax := readControllerAxis(3) // right stick Y
	rUp := keyDown(glfw.KeyUp)
	rDn := keyDown(glfw.KeyDown)
	if rax != 0 || rUp || rDn {
		g.rightHumanActive = true
	}
	if g.state == stateServe {
		// release control back to AI on each new serve so a stick that's
		// been let go doesn't keep AI suppressed forever
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
		// Simple AI: track ball Y when it's heading towards us, otherwise
		// drift back toward centre.  Capped speed keeps it beatable.
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

	// ── Ball ──
	if g.state != statePlay {
		return
	}
	g.bx += g.bvx * dt
	g.by += g.bvy * dt

	// top/bottom
	if g.by < 0 {
		g.by = 0
		g.bvy = -g.bvy
	} else if g.by+ballSize > winH {
		g.by = winH - ballSize
		g.bvy = -g.bvy
	}

	// paddle collisions
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

	// scoring
	if g.bx+ballSize < 0 {
		g.rightScore++
		g.afterPoint()
	} else if g.bx > winW {
		g.leftScore++
		g.afterPoint()
	}
}

func (g *game) bounceOffPaddle(paddleTop, dir float64) {
	// contact in [-1, +1]: -1 = top edge, +1 = bottom edge
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

// ─── Draw ────────────────────────────────────────────────────────────────────

func (g *game) draw(r *renderer) {
	r.begin()

	// Centre net (dashes)
	for y := 0.0; y < winH; y += 24 {
		r.rect(winW/2-2, float32(y)+4, 4, 14, 0.6, 0.6, 0.6)
	}

	// Paddles + ball
	r.rect(paddleMargin, float32(g.leftY), paddleW, paddleH, 1, 1, 1)
	r.rect(winW-paddleMargin-paddleW, float32(g.rightY), paddleW, paddleH, 1, 1, 1)
	r.rect(float32(g.bx), float32(g.by), ballSize, ballSize, 1, 1, 1)

	// Score, drawn 7-segment-style at the top
	drawNumber(r, winW/4-30, 40, 40, 60, 6, g.leftScore)
	drawNumber(r, winW*3/4-30, 40, 40, 60, 6, g.rightScore)

	// Match over: dim the screen by drawing a half-transparent black quad
	// (we don't have blending enabled, so fake it with a solid grey wash on
	// the score region only)
	if g.state == stateMatchOver {
		var msg string
		if g.leftScore > g.rightScore {
			msg = "L WIN"
		} else {
			msg = "R WIN"
		}
		drawText(r, winW/2-len(msg)*30/2, winH/2+60, 26, 40, 4, msg)
	}
}

// ─── Tiny 7-segment digit/letter renderer ────────────────────────────────────
//
// segments laid out:
//      aaaa
//     f    b
//     f    b
//      gggg
//     e    c
//     e    c
//      dddd

var glyphs = map[rune]uint8{
	'0': 0b1111110,
	'1': 0b0110000,
	'2': 0b1101101,
	'3': 0b1111001,
	'4': 0b0110011,
	'5': 0b1011011,
	'6': 0b1011111,
	'7': 0b1110000,
	'8': 0b1111111,
	'9': 0b1111011,
	'L': 0b0001110,
	'W': 0b0101110, // approximate
	'I': 0b0110000,
	'N': 0b1110110, // approximate
	'R': 0b1100111, // approximate
	' ': 0,
}

func drawSegment(r *renderer, seg int, x, y, w, h, t float32) {
	switch seg {
	case 0: // a
		r.rect(x, y, w, t, 1, 1, 1)
	case 1: // b
		r.rect(x+w-t, y, t, h/2, 1, 1, 1)
	case 2: // c
		r.rect(x+w-t, y+h/2, t, h/2, 1, 1, 1)
	case 3: // d
		r.rect(x, y+h-t, w, t, 1, 1, 1)
	case 4: // e
		r.rect(x, y+h/2, t, h/2, 1, 1, 1)
	case 5: // f
		r.rect(x, y, t, h/2, 1, 1, 1)
	case 6: // g
		r.rect(x, y+h/2-t/2, w, t, 1, 1, 1)
	}
}

func drawDigit(r *renderer, x, y, w, h, t float32, d rune) {
	bits, ok := glyphs[d]
	if !ok {
		return
	}
	for i := 0; i < 7; i++ {
		if bits&(1<<(6-i)) != 0 {
			drawSegment(r, i, x, y, w, h, t)
		}
	}
}

func drawNumber(r *renderer, x, y, w, h, t int, n int) {
	s := fmt.Sprintf("%d", n)
	for i, c := range s {
		drawDigit(r, float32(x+i*(w+10)), float32(y), float32(w), float32(h), float32(t), c)
	}
}

func drawText(r *renderer, x, y, w, h, t int, s string) {
	for i, c := range s {
		drawDigit(r, float32(x+i*(w+10)), float32(y), float32(w), float32(h), float32(t), c)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
