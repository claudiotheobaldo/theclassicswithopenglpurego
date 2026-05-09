// Asteroids — third classic in the suite.
//
// Vector-style top-down shooter: rotate, thrust with momentum, shoot rocks,
// hyperspace out of trouble.  All asteroids destroyed → next wave with one
// more starting rock.  Three lives, screen wraps on every edge.
//
// Per the original 1979 Atari rules:
//   - Large asteroid hit → splits into two medium
//   - Medium asteroid hit → splits into two small
//   - Small asteroid hit → destroyed
//   - Bullets travel a fixed range and expire (do not wrap)
//   - Ship has inertia: thrust adds velocity, no thrust = drift forever
//   - Hyperspace teleports the ship to a random spot, with a small risk of
//     reappearing on top of a rock
//
// Controls
//   Keyboard
//     A / Left   : rotate counter-clockwise
//     D / Right  : rotate clockwise
//     W / Up     : thrust
//     Space      : fire
//     H          : hyperspace
//     Enter      : start / restart
//     Esc        : quit
//   Gamepad (Xbox-style XInput)
//     Left stick X      : rotate (analog)
//     Right trigger     : thrust (analog)
//     A button (idx 0)  : fire
//     B button (idx 1)  : hyperspace
//     Start (idx 7)     : start / restart
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
	winW, winH = 900, 700

	// ship
	shipRotSpeed     = 4.5   // rad/s
	shipThrustAccel  = 220.0 // px/s^2
	shipMaxSpeed     = 360.0
	shipFriction     = 0.35 // per second
	shipRadius       = 12.0
	shipFireCooldown = 0.18
	shipInvincible   = 2.0 // seconds after spawn
	hyperRespawnTime = 0.6

	// bullet
	bulletSpeed   = 540.0
	bulletLife    = 0.85
	bulletRadius  = 2.0
	maxBullets    = 4

	// asteroids
	astBigR     = 42.0
	astMedR     = 22.0
	astSmallR   = 11.0
	astSpeedMin = 30.0
	astSpeedMax = 90.0

	// scoring
	scoreBig   = 20
	scoreMed   = 50
	scoreSmall = 100

	startLives = 3
	startWaveSize = 4
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

	win, err := glfw.CreateWindow(winW, winH, "Asteroids", nil, nil)
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

	// One-shot edge-triggered actions go through the key callback so a tap
	// always registers exactly once even if the frame is long.  Continuous
	// actions (rotate, thrust) read held state via GetKey.
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press {
			return
		}
		switch key {
		case glfw.KeySpace:
			g.tryFire()
		case glfw.KeyH:
			g.tryHyperspace()
		case glfw.KeyEnter, glfw.KeyKPEnter:
			if g.state != statePlay {
				g.reset()
			}
		case glfw.KeyF11:
			winutil.ToggleFullscreen(win)
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		}
	})

	// Aspect-fit the playfield to whatever the framebuffer happens to be
	// (window resize, fullscreen toggle, etc.).  Without this the rocks
	// stretch into ovals on a wider monitor.
	fbW, fbH := win.GetFramebufferSize()
	applyViewport := func(w, h int) {
		x, y, vw, vh := winutil.LetterboxRect(w, h, winW, winH)
		gl.Viewport(int32(x), int32(y), int32(vw), int32(vh))
	}
	applyViewport(fbW, fbH)
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		fbW, fbH = w, h
		applyViewport(w, h)
	})
	_ = fbW
	_ = fbH

	last := time.Now()
	for !win.ShouldClose() {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		if dt > 0.05 {
			dt = 0.05
		}
		last = now

		g.input(win)
		g.update(dt)

		gl.ClearColor(0.02, 0.02, 0.04, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.Begin(winW, winH)
		g.draw(r)

		win.SwapBuffers()
		glfw.PollEvents()
	}
}

// ─── Game state ──────────────────────────────────────────────────────────────

type vec2 struct{ x, y float64 }

func (a vec2) add(b vec2) vec2     { return vec2{a.x + b.x, a.y + b.y} }
func (a vec2) sub(b vec2) vec2     { return vec2{a.x - b.x, a.y - b.y} }
func (a vec2) scale(s float64) vec2 { return vec2{a.x * s, a.y * s} }
func (a vec2) lenSq() float64       { return a.x*a.x + a.y*a.y }

type ship struct {
	pos, vel      vec2
	angle         float64 // radians, 0 = +X
	alive         bool
	respawnTime   float64
	invincTime    float64
	thrustingHint bool // set each frame by input(), read by draw()
}

type bullet struct {
	pos, vel vec2
	life     float64
}

type asteroid struct {
	pos, vel vec2
	radius   float64
	rotation float64 // for visual spin
	rotSpeed float64
	shape    [][2]float32 // pre-computed jagged outline in local space (around origin)
}

type gameState int

const (
	stateAttract gameState = iota
	statePlay
	stateGameOver
)

type game struct {
	rng         *rand.Rand
	state       gameState
	ship        ship
	bullets     []bullet
	asteroids   []asteroid
	wave        int
	score       int
	highScore   int
	lives       int
	fireCD      float64
	prevButtons []glfw.Action // edge-detect controller buttons
}

func newGame() *game {
	g := &game{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	g.state = stateAttract
	g.spawnAsteroids(startWaveSize) // attract-mode demo: just rocks tumbling
	return g
}

func (g *game) reset() {
	g.score = 0
	g.lives = startLives
	g.wave = 1
	g.bullets = g.bullets[:0]
	g.asteroids = g.asteroids[:0]
	g.spawnAsteroids(startWaveSize)
	g.respawnShip()
	g.state = statePlay
}

func (g *game) respawnShip() {
	g.ship = ship{
		pos:        vec2{winW / 2, winH / 2},
		angle:      -math.Pi / 2, // pointing up
		alive:      true,
		invincTime: shipInvincible,
	}
}

// ─── Input ───────────────────────────────────────────────────────────────────

func (g *game) input(win *glfw.Window) {
	// Continuous keyboard.
	keyDown := func(k glfw.Key) bool { return win.GetKey(k) == glfw.Press }
	rot := 0.0
	if keyDown(glfw.KeyA) || keyDown(glfw.KeyLeft) {
		rot -= 1
	}
	if keyDown(glfw.KeyD) || keyDown(glfw.KeyRight) {
		rot += 1
	}
	thrust := 0.0
	if keyDown(glfw.KeyW) || keyDown(glfw.KeyUp) {
		thrust = 1
	}

	// Gamepad: analog stick X for rotation, right trigger for thrust.
	if joy, ok := firstJoystick(); ok {
		ax := glfw.GetJoystickAxes(joy)
		if len(ax) > 0 {
			v := float64(ax[0])
			if math.Abs(v) > 0.18 {
				rot += v
			}
		}
		// Right trigger is axis index 5 in our XInput mapping.  Range is
		// [-1, +1] where -1 = released, +1 = fully pressed.
		if len(ax) > 5 {
			t := (float64(ax[5]) + 1) * 0.5 // -> [0, 1]
			if t > 0.1 {
				thrust = math.Max(thrust, t)
			}
		}

		// A button = fire, B = hyperspace, Start = restart.  Edge-detect.
		btns := glfw.GetJoystickButtons(joy)
		if g.prevButtons == nil || len(g.prevButtons) != len(btns) {
			g.prevButtons = make([]glfw.Action, len(btns))
		}
		fire := btns[0] == glfw.Press && g.prevButtons[0] != glfw.Press
		hyper := len(btns) > 1 && btns[1] == glfw.Press && g.prevButtons[1] != glfw.Press
		start := len(btns) > 7 && btns[7] == glfw.Press && g.prevButtons[7] != glfw.Press
		copy(g.prevButtons, btns)

		if fire {
			g.tryFire()
		}
		if hyper {
			g.tryHyperspace()
		}
		if start && g.state != statePlay {
			g.reset()
		}
	}

	if rot < -1 {
		rot = -1
	}
	if rot > 1 {
		rot = 1
	}
	g.ship.angle += rot * shipRotSpeed * dtNow()
	if thrust > 0 && g.ship.alive && g.state == statePlay {
		ax := math.Cos(g.ship.angle) * shipThrustAccel * thrust * dtNow()
		ay := math.Sin(g.ship.angle) * shipThrustAccel * thrust * dtNow()
		g.ship.vel.x += ax
		g.ship.vel.y += ay
		g.ship.thrustingHint = true
	} else {
		g.ship.thrustingHint = false
	}
}

// dtNow is a hack to share the current frame dt with input(); update() sets
// it before calling input via a package-level variable.  Lets input() apply
// rotation/thrust frame-rate-independently without restructuring the main
// loop.
var lastDt float64

func dtNow() float64 { return lastDt }

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
	lastDt = dt
	switch g.state {
	case stateAttract:
		g.updateAsteroids(dt)
		return
	case stateGameOver:
		g.updateAsteroids(dt)
		return
	}

	// Ship.
	if g.ship.alive {
		g.ship.invincTime -= dt
		// friction-ish drag
		drag := math.Pow(1.0-shipFriction, dt)
		g.ship.vel = g.ship.vel.scale(drag)
		// clamp speed
		sp := math.Sqrt(g.ship.vel.lenSq())
		if sp > shipMaxSpeed {
			g.ship.vel = g.ship.vel.scale(shipMaxSpeed / sp)
		}
		g.ship.pos = g.ship.pos.add(g.ship.vel.scale(dt))
		wrap(&g.ship.pos)
	} else {
		g.ship.respawnTime -= dt
		if g.ship.respawnTime <= 0 && g.lives > 0 {
			g.respawnShip()
		}
	}

	// Bullets.
	g.fireCD -= dt
	out := g.bullets[:0]
	for _, b := range g.bullets {
		b.pos = b.pos.add(b.vel.scale(dt))
		b.life -= dt
		// Bullets do NOT wrap in the original Atari game — they expire by
		// range.  But we wrap to keep the screen feeling continuous; range
		// is enforced via life timer.
		wrap(&b.pos)
		if b.life > 0 {
			out = append(out, b)
		}
	}
	g.bullets = out

	g.updateAsteroids(dt)
	g.handleCollisions()

	// Wave clear.
	if len(g.asteroids) == 0 {
		g.wave++
		g.spawnAsteroids(startWaveSize + g.wave - 1)
	}
}

func (g *game) updateAsteroids(dt float64) {
	for i := range g.asteroids {
		a := &g.asteroids[i]
		a.pos = a.pos.add(a.vel.scale(dt))
		a.rotation += a.rotSpeed * dt
		wrap(&a.pos)
	}
}

func (g *game) handleCollisions() {
	// Bullet vs asteroid.
	for bi := 0; bi < len(g.bullets); bi++ {
		b := g.bullets[bi]
		for ai := 0; ai < len(g.asteroids); ai++ {
			a := g.asteroids[ai]
			rr := (a.radius + bulletRadius)
			if b.pos.sub(a.pos).lenSq() < rr*rr {
				g.splitAsteroid(ai)
				// remove bullet
				g.bullets = append(g.bullets[:bi], g.bullets[bi+1:]...)
				bi--
				break
			}
		}
	}

	// Ship vs asteroid.
	if g.ship.alive && g.ship.invincTime <= 0 {
		for _, a := range g.asteroids {
			rr := a.radius + shipRadius
			if g.ship.pos.sub(a.pos).lenSq() < rr*rr {
				g.killShip()
				return
			}
		}
	}
}

func (g *game) splitAsteroid(i int) {
	a := g.asteroids[i]
	switch {
	case a.radius > astMedR+1:
		g.score += scoreBig
		g.asteroids[i] = g.makeAsteroid(a.pos, astMedR)
		g.asteroids = append(g.asteroids, g.makeAsteroid(a.pos, astMedR))
	case a.radius > astSmallR+1:
		g.score += scoreMed
		g.asteroids[i] = g.makeAsteroid(a.pos, astSmallR)
		g.asteroids = append(g.asteroids, g.makeAsteroid(a.pos, astSmallR))
	default:
		g.score += scoreSmall
		g.asteroids = append(g.asteroids[:i], g.asteroids[i+1:]...)
	}
	if g.score > g.highScore {
		g.highScore = g.score
	}
}

func (g *game) killShip() {
	g.ship.alive = false
	g.ship.respawnTime = hyperRespawnTime
	g.lives--
	if g.lives <= 0 {
		g.state = stateGameOver
		fmt.Printf("Game over. Score: %d (high: %d)\n", g.score, g.highScore)
	}
}

func (g *game) tryFire() {
	if g.state != statePlay || !g.ship.alive || g.fireCD > 0 || len(g.bullets) >= maxBullets {
		return
	}
	g.fireCD = shipFireCooldown
	dir := vec2{math.Cos(g.ship.angle), math.Sin(g.ship.angle)}
	g.bullets = append(g.bullets, bullet{
		pos:  g.ship.pos.add(dir.scale(shipRadius + 4)),
		vel:  g.ship.vel.add(dir.scale(bulletSpeed)),
		life: bulletLife,
	})
}

func (g *game) tryHyperspace() {
	if g.state != statePlay || !g.ship.alive {
		return
	}
	g.ship.pos = vec2{
		x: 30 + g.rng.Float64()*(winW-60),
		y: 30 + g.rng.Float64()*(winH-60),
	}
	g.ship.vel = vec2{}
	// Classic hyperspace risk: small chance of immediate death.  We model
	// it by clearing invincibility for a frame so any rock you teleport
	// onto kills you.
	g.ship.invincTime = 0
}

// ─── Asteroid spawning ──────────────────────────────────────────────────────

func (g *game) spawnAsteroids(n int) {
	for i := 0; i < n; i++ {
		// Spawn at edge so they drift inward, away from the ship's spawn.
		var p vec2
		switch g.rng.Intn(4) {
		case 0:
			p = vec2{0, g.rng.Float64() * winH}
		case 1:
			p = vec2{winW, g.rng.Float64() * winH}
		case 2:
			p = vec2{g.rng.Float64() * winW, 0}
		case 3:
			p = vec2{g.rng.Float64() * winW, winH}
		}
		g.asteroids = append(g.asteroids, g.makeAsteroid(p, astBigR))
	}
}

func (g *game) makeAsteroid(pos vec2, radius float64) asteroid {
	speed := astSpeedMin + g.rng.Float64()*(astSpeedMax-astSpeedMin)
	// Smaller rocks travel faster.
	speed *= astBigR / radius * 0.6
	dir := g.rng.Float64() * 2 * math.Pi
	a := asteroid{
		pos:      pos,
		vel:      vec2{math.Cos(dir) * speed, math.Sin(dir) * speed},
		radius:   radius,
		rotSpeed: (g.rng.Float64() - 0.5) * 1.6,
	}
	// Pre-compute jagged outline.
	const verts = 11
	a.shape = make([][2]float32, verts)
	for i := 0; i < verts; i++ {
		theta := float64(i) / verts * 2 * math.Pi
		jitter := 0.8 + g.rng.Float64()*0.4
		r := radius * jitter
		a.shape[i] = [2]float32{
			float32(math.Cos(theta) * r),
			float32(math.Sin(theta) * r),
		}
	}
	return a
}

// wrap moves p to the opposite edge if it leaves the playfield.
func wrap(p *vec2) {
	if p.x < 0 {
		p.x += winW
	} else if p.x >= winW {
		p.x -= winW
	}
	if p.y < 0 {
		p.y += winH
	} else if p.y >= winH {
		p.y -= winH
	}
}

// ─── Draw ────────────────────────────────────────────────────────────────────

func (g *game) draw(r *render.Renderer) {
	// HUD.
	r.Number(20, 16, 18, 26, 0, g.score, 1, 1, 1)
	for i := 0; i < g.lives; i++ {
		drawShipIcon(r, float32(140+i*22), 26)
	}
	r.Text(winW-180, 16, 14, 20, 0, "WAVE", 0.7, 0.7, 0.7)
	r.Number(winW-90, 16, 18, 26, 0, g.wave, 1, 1, 1)

	// Asteroids.
	for _, a := range g.asteroids {
		drawAsteroid(r, a)
	}

	// Ship.
	if g.ship.alive {
		blink := g.ship.invincTime > 0 && int(g.ship.invincTime*8)%2 == 0
		if !blink {
			drawShip(r, g.ship)
		}
	}

	// Bullets — small bright dots.
	for _, b := range g.bullets {
		r.Rect(float32(b.pos.x)-2, float32(b.pos.y)-2, 4, 4, 1, 1, 1)
	}

	// Overlays.
	switch g.state {
	case stateAttract:
		drawCenteredText(r, "ASTEROIDS", winH/2-40, 30, 44, 0)
		drawCenteredText(r, "ENTER OR START TO BEGIN", winH/2+40, 14, 20, 0)
	case stateGameOver:
		drawCenteredText(r, "GAME OVER", winH/2-30, 30, 44, 0)
		drawCenteredText(r, "ENTER OR START TO RESTART", winH/2+30, 14, 20, 0)
	}
}

func drawCenteredText(r *render.Renderer, s string, y int, w, h, t float32) {
	const gap = 10
	stride := w + gap
	totalW := float32(len(s))*stride - gap
	r.Text(float32(winW)/2-totalW/2, float32(y), w, h, t, s, 1, 1, 1)
}

func drawShip(r *render.Renderer, s ship) {
	// Local-space triangle pointing along +X (matches angle convention).
	nose := [2]float64{16, 0}
	left := [2]float64{-10, -8}
	right := [2]float64{-10, 8}
	c, sn := math.Cos(s.angle), math.Sin(s.angle)
	rot := func(p [2]float64) [2]float32 {
		return [2]float32{
			float32(s.pos.x + p[0]*c - p[1]*sn),
			float32(s.pos.y + p[0]*sn + p[1]*c),
		}
	}
	verts := [][2]float32{rot(nose), rot(left), rot(right)}
	r.PolygonStroke(verts, 2, 1, 1, 1, true)

	// Thrust flame.
	if s.thrustingHint && time.Now().UnixNano()%2 == 0 {
		flame := [][2]float32{
			rot([2]float64{-10, -4}),
			rot([2]float64{-18, 0}),
			rot([2]float64{-10, 4}),
		}
		r.PolygonStroke(flame, 1.5, 1, 0.6, 0.2, false)
	}
}

func drawShipIcon(r *render.Renderer, cx, cy float32) {
	verts := [][2]float32{
		{cx + 8, cy},
		{cx - 5, cy - 5},
		{cx - 5, cy + 5},
	}
	r.PolygonStroke(verts, 1.5, 1, 1, 1, true)
}

func drawAsteroid(r *render.Renderer, a asteroid) {
	c, sn := math.Cos(a.rotation), math.Sin(a.rotation)
	verts := make([][2]float32, len(a.shape))
	for i, p := range a.shape {
		x, y := float64(p[0]), float64(p[1])
		verts[i] = [2]float32{
			float32(a.pos.x + x*c - y*sn),
			float32(a.pos.y + x*sn + y*c),
		}
	}
	r.PolygonStroke(verts, 1.8, 0.85, 0.85, 0.9, true)
}
