// Dungeoncrawl — first-person 3D maze.  First entry in the suite that
// breaks 2D and exercises every remaining gl-purego pillar at once:
//
//   - glEnable(DEPTH_TEST) + per-window depth buffer
//   - Perspective + view + model matrix uniforms (glUniformMatrix4fv)
//   - 3 vertex attributes per vertex (pos, normal, color) with strided
//     interleaved layout — every prior consumer used a single attribute
//   - Index buffer (EBO) drawn via glDrawElements; up to now only
//     glDrawArrays / glDrawArraysInstanced have been used
//   - Diffuse lighting in the fragment shader (normal · sun)
//
// And on the input side:
//
//   - SetInputMode(CursorMode, CursorDisabled) for cursor capture
//   - RawMouseMotionSupported + SetInputMode(RawMouseMotion, True) so
//     the OS mouse-acceleration curve doesn't smear the look direction
//   - Continuous mouse delta tracking via SetCursorPosCallback
//
// Plus F12 reuses internal/screenshot.
//
// Controls
//   Mouse        : look (after the first click captures the cursor)
//   W / A / S / D: walk
//   Shift        : sprint
//   Esc          : release cursor (click the window again to recapture)
//   F11          : fullscreen
//   F12          : screenshot
//   Q            : quit
package main

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/screenshot"
	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/winutil"
)

const (
	winW = 1000
	winH = 700

	playerRadius = 0.30
	walkSpeed    = float32(3.5)
	sprintMul    = float32(1.7)
	mouseSens    = 0.0025
	eyeHeight    = 0.7
)

// 16×16 dungeon: # = wall, . = floor, + = pillar (smaller decorative cube).
const mapStr = `################
#..............#
#..#...........#
#..#.....++....#
#........++....#
#..............#
#......++......#
#......++......#
#..............#
#..#...........#
#..#.......#...#
#..........#...#
#.+........#...#
#.+............#
#..............#
################`

const mapW, mapH = 16, 16

func init() { runtime.LockOSThread() }

// ─── vec3 / mat4 helpers ─────────────────────────────────────────────────────

type vec3 struct{ x, y, z float32 }

func v3(x, y, z float32) vec3 { return vec3{x, y, z} }

func (a vec3) add(b vec3) vec3   { return vec3{a.x + b.x, a.y + b.y, a.z + b.z} }
func (a vec3) sub(b vec3) vec3   { return vec3{a.x - b.x, a.y - b.y, a.z - b.z} }
func (a vec3) scale(s float32) vec3 { return vec3{a.x * s, a.y * s, a.z * s} }
func (a vec3) dot(b vec3) float32 { return a.x*b.x + a.y*b.y + a.z*b.z }
func (a vec3) cross(b vec3) vec3 {
	return vec3{
		a.y*b.z - a.z*b.y,
		a.z*b.x - a.x*b.z,
		a.x*b.y - a.y*b.x,
	}
}
func (a vec3) length() float32 { return float32(math.Sqrt(float64(a.dot(a)))) }
func (a vec3) normalize() vec3 {
	l := a.length()
	if l == 0 {
		return a
	}
	return a.scale(1 / l)
}

// mat4 is column-major (OpenGL convention).  Stored row 0..3 of column 0,
// then row 0..3 of column 1, etc., so the layout matches glUniformMatrix4fv
// with transpose=false.
type mat4 [16]float32

func mat4Identity() mat4 {
	return mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

func mat4Translate(t vec3) mat4 {
	m := mat4Identity()
	m[12], m[13], m[14] = t.x, t.y, t.z
	return m
}

func mat4Scale(s vec3) mat4 {
	return mat4{
		s.x, 0, 0, 0,
		0, s.y, 0, 0,
		0, 0, s.z, 0,
		0, 0, 0, 1,
	}
}

func mat4Mul(a, b mat4) mat4 {
	var r mat4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			var s float32
			for k := 0; k < 4; k++ {
				s += a[k*4+row] * b[col*4+k]
			}
			r[col*4+row] = s
		}
	}
	return r
}

// mat4Perspective builds an OpenGL-style symmetric perspective matrix
// matching the GL clip-space convention (NDC z in [-1, 1]).
func mat4Perspective(fovY, aspect, nearZ, farZ float32) mat4 {
	f := float32(1.0 / math.Tan(float64(fovY)/2))
	return mat4{
		f / aspect, 0, 0, 0,
		0, f, 0, 0,
		0, 0, (farZ + nearZ) / (nearZ - farZ), -1,
		0, 0, (2 * farZ * nearZ) / (nearZ - farZ), 0,
	}
}

// mat4LookAt is the standard right-handed look-at matrix.
func mat4LookAt(eye, target, up vec3) mat4 {
	f := target.sub(eye).normalize()
	s := f.cross(up).normalize()
	u := s.cross(f)
	return mat4{
		s.x, u.x, -f.x, 0,
		s.y, u.y, -f.y, 0,
		s.z, u.z, -f.z, 0,
		-s.dot(eye), -u.dot(eye), f.dot(eye), 1,
	}
}

// ─── Cube geometry ───────────────────────────────────────────────────────────
//
// 24 vertices, 4 per face × 6 faces.  Each vertex carries position,
// face normal, and a unit colour (1,1,1).  Per-cube tint is applied via
// uniform; lighting via the fragment shader's diffuse term.

var cubeVerts = []float32{
	// +X face
	0.5, -0.5, -0.5, 1, 0, 0, 1, 1, 1,
	0.5, 0.5, -0.5, 1, 0, 0, 1, 1, 1,
	0.5, 0.5, 0.5, 1, 0, 0, 1, 1, 1,
	0.5, -0.5, 0.5, 1, 0, 0, 1, 1, 1,
	// -X face
	-0.5, -0.5, 0.5, -1, 0, 0, 1, 1, 1,
	-0.5, 0.5, 0.5, -1, 0, 0, 1, 1, 1,
	-0.5, 0.5, -0.5, -1, 0, 0, 1, 1, 1,
	-0.5, -0.5, -0.5, -1, 0, 0, 1, 1, 1,
	// +Y face
	-0.5, 0.5, -0.5, 0, 1, 0, 1, 1, 1,
	-0.5, 0.5, 0.5, 0, 1, 0, 1, 1, 1,
	0.5, 0.5, 0.5, 0, 1, 0, 1, 1, 1,
	0.5, 0.5, -0.5, 0, 1, 0, 1, 1, 1,
	// -Y face
	-0.5, -0.5, 0.5, 0, -1, 0, 1, 1, 1,
	-0.5, -0.5, -0.5, 0, -1, 0, 1, 1, 1,
	0.5, -0.5, -0.5, 0, -1, 0, 1, 1, 1,
	0.5, -0.5, 0.5, 0, -1, 0, 1, 1, 1,
	// +Z face
	0.5, -0.5, 0.5, 0, 0, 1, 1, 1, 1,
	0.5, 0.5, 0.5, 0, 0, 1, 1, 1, 1,
	-0.5, 0.5, 0.5, 0, 0, 1, 1, 1, 1,
	-0.5, -0.5, 0.5, 0, 0, 1, 1, 1, 1,
	// -Z face
	-0.5, -0.5, -0.5, 0, 0, -1, 1, 1, 1,
	-0.5, 0.5, -0.5, 0, 0, -1, 1, 1, 1,
	0.5, 0.5, -0.5, 0, 0, -1, 1, 1, 1,
	0.5, -0.5, -0.5, 0, 0, -1, 1, 1, 1,
}

var cubeIndices = []uint32{
	0, 1, 2, 0, 2, 3,
	4, 5, 6, 4, 6, 7,
	8, 9, 10, 8, 10, 11,
	12, 13, 14, 12, 14, 15,
	16, 17, 18, 16, 18, 19,
	20, 21, 22, 20, 22, 23,
}

// ─── Game state ──────────────────────────────────────────────────────────────

type game struct {
	posX, posZ float32
	yaw        float32 // radians, 0 = +X
	pitch      float32

	mouseCaptured bool
	firstMouse    bool
	lastMX, lastMY float64
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
	glfw.WindowHint(glfw.Resizable, glfw.True)
	glfw.WindowHint(glfw.DepthBits, 24) // first consumer asking for a depth buffer

	win, err := glfw.CreateWindow(winW, winH, "Dungeoncrawl", nil, nil)
	if err != nil {
		panic(err)
	}
	win.MakeContextCurrent()
	glfw.SwapInterval(1)
	if err := gl.Init(); err != nil {
		panic(err)
	}

	gl.Enable(gl.DEPTH_TEST)
	gl.Enable(gl.CULL_FACE)
	gl.CullFace(gl.BACK)
	gl.FrontFace(gl.CCW)

	prog := compileProgram(vsSrc, fsSrc)
	defer gl.DeleteProgram(prog)

	uModel := gl.GetUniformLocation(prog, gl.Str("uModel\x00"))
	uView := gl.GetUniformLocation(prog, gl.Str("uView\x00"))
	uProj := gl.GetUniformLocation(prog, gl.Str("uProj\x00"))
	uTint := gl.GetUniformLocation(prog, gl.Str("uTint\x00"))
	uSunDir := gl.GetUniformLocation(prog, gl.Str("uSunDir\x00"))

	// One VAO; VBO holds interleaved pos+normal+color; EBO holds indices.
	var vao, vbo, ebo uint32
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(cubeVerts)*4, gl.Ptr(cubeVerts), gl.STATIC_DRAW)
	gl.GenBuffers(1, &ebo)
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, ebo)
	gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(cubeIndices)*4, gl.Ptr(cubeIndices), gl.STATIC_DRAW)

	const stride int32 = 9 * 4
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 3, gl.FLOAT, false, stride, 0)
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointerWithOffset(1, 3, gl.FLOAT, false, stride, 3*4)
	gl.EnableVertexAttribArray(2)
	gl.VertexAttribPointerWithOffset(2, 3, gl.FLOAT, false, stride, 6*4)

	g := newGame()

	// Cursor capture and raw motion.
	captureCursor := func() {
		win.SetInputMode(glfw.CursorMode, glfw.CursorDisabled)
		if glfw.RawMouseMotionSupported() {
			win.SetInputMode(glfw.RawMouseMotion, glfw.True)
		}
		g.mouseCaptured = true
		g.firstMouse = true
	}
	releaseCursor := func() {
		win.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
		g.mouseCaptured = false
	}
	captureCursor()

	win.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		if !g.mouseCaptured {
			return
		}
		if g.firstMouse {
			g.lastMX, g.lastMY = x, y
			g.firstMouse = false
			return
		}
		dx := x - g.lastMX
		dy := y - g.lastMY
		g.lastMX, g.lastMY = x, y
		g.yaw += float32(dx) * mouseSens
		g.pitch -= float32(dy) * mouseSens
		// Clamp pitch so the camera can't flip over.
		const lim = float32(math.Pi/2 - 0.05)
		if g.pitch > lim {
			g.pitch = lim
		}
		if g.pitch < -lim {
			g.pitch = -lim
		}
	})

	win.SetMouseButtonCallback(func(_ *glfw.Window, btn glfw.MouseButton, action glfw.Action, _ glfw.ModifierKey) {
		if btn == glfw.MouseButtonLeft && action == glfw.Press && !g.mouseCaptured {
			captureCursor()
		}
	})

	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press {
			return
		}
		switch key {
		case glfw.KeyEscape:
			if g.mouseCaptured {
				releaseCursor()
			} else {
				win.SetShouldClose(true)
			}
		case glfw.KeyQ:
			win.SetShouldClose(true)
		case glfw.KeyF11:
			winutil.ToggleFullscreen(win)
		case glfw.KeyF12:
			fbW, fbH := win.GetFramebufferSize()
			if name, err := screenshot.Save("dungeoncrawl", fbW, fbH); err != nil {
				fmt.Println("screenshot:", err)
			} else {
				fmt.Println("saved", name)
			}
		}
	})

	fbW, fbH := win.GetFramebufferSize()
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		fbW, fbH = w, h
		gl.Viewport(0, 0, int32(w), int32(h))
	})

	// Pre-compute the list of solid cells from the map so we don't walk
	// the string every frame.
	walls := []vec3{}
	pillars := []vec3{}
	rows := strings.Split(strings.TrimSpace(mapStr), "\n")
	for z, row := range rows {
		for x, c := range row {
			switch c {
			case '#':
				walls = append(walls, v3(float32(x)+0.5, 0.5, float32(z)+0.5))
			case '+':
				pillars = append(pillars, v3(float32(x)+0.5, 0.3, float32(z)+0.5))
			}
		}
	}

	last := time.Now()
	sunDir := v3(-0.4, 0.8, -0.5).normalize()
	for !win.ShouldClose() {
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		if dt > 0.05 {
			dt = 0.05
		}
		last = now

		g.move(win, dt)

		gl.ClearColor(0.05, 0.06, 0.10, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

		gl.UseProgram(prog)
		gl.BindVertexArray(vao)

		eye := v3(g.posX, eyeHeight, g.posZ)
		fwd := g.forward()
		view := mat4LookAt(eye, eye.add(fwd), v3(0, 1, 0))
		proj := mat4Perspective(float32(70*math.Pi/180), float32(fbW)/float32(fbH), 0.05, 60)

		gl.UniformMatrix4fv(uView, 1, false, &view[0])
		gl.UniformMatrix4fv(uProj, 1, false, &proj[0])
		gl.Uniform3f(uSunDir, sunDir.x, sunDir.y, sunDir.z)

		// Floor: stretched cube.
		drawCube(uModel, uTint,
			mat4Mul(mat4Translate(v3(mapW/2, -0.025, mapH/2)),
				mat4Scale(v3(mapW, 0.05, mapH))),
			v3(0.20, 0.22, 0.18))

		// Walls.
		for _, p := range walls {
			drawCube(uModel, uTint, mat4Translate(p), v3(0.50, 0.52, 0.55))
		}

		// Pillars (shorter, warmer-coloured).
		for _, p := range pillars {
			drawCube(uModel, uTint,
				mat4Mul(mat4Translate(p), mat4Scale(v3(0.5, 0.6, 0.5))),
				v3(0.60, 0.45, 0.30))
		}

		win.SwapBuffers()
		glfw.PollEvents()
	}
}

func newGame() *game {
	g := &game{}
	g.posX, g.posZ = mapW/2, mapH/2 - 1
	g.yaw = 0
	g.pitch = 0
	return g
}

// forward returns the unit vector the camera is looking along.
func (g *game) forward() vec3 {
	cy := float32(math.Cos(float64(g.yaw)))
	sy := float32(math.Sin(float64(g.yaw)))
	cp := float32(math.Cos(float64(g.pitch)))
	sp := float32(math.Sin(float64(g.pitch)))
	return v3(cy*cp, sp, sy*cp)
}

// flatForward is forward with the y component zeroed and re-normalised —
// the direction WASD walks along.  Stops the player from gliding into
// the floor when looking down.
func (g *game) flatForward() vec3 {
	f := g.forward()
	f.y = 0
	return f.normalize()
}

func (g *game) move(win *glfw.Window, dt float32) {
	keyDown := func(k glfw.Key) bool { return win.GetKey(k) == glfw.Press }
	speed := walkSpeed
	if keyDown(glfw.KeyLeftShift) || keyDown(glfw.KeyRightShift) {
		speed *= sprintMul
	}
	dir := vec3{}
	fwd := g.flatForward()
	right := fwd.cross(v3(0, 1, 0)).normalize()
	if keyDown(glfw.KeyW) {
		dir = dir.add(fwd)
	}
	if keyDown(glfw.KeyS) {
		dir = dir.sub(fwd)
	}
	if keyDown(glfw.KeyD) {
		dir = dir.add(right)
	}
	if keyDown(glfw.KeyA) {
		dir = dir.sub(right)
	}
	if dir.length() == 0 {
		return
	}
	dir = dir.normalize().scale(speed * dt)

	// Resolve X and Z separately so sliding along walls feels right.
	if !blocked(g.posX+dir.x, g.posZ) {
		g.posX += dir.x
	}
	if !blocked(g.posX, g.posZ+dir.z) {
		g.posZ += dir.z
	}
}

// blocked tests whether a player-radius circle at (x, z) overlaps a wall
// cell.  Pillars don't block — they're shorter than wall height so it
// looks weird if you can't squeeze past, plus the test exercises the
// "decorative geometry" pattern.
func blocked(x, z float32) bool {
	const r = playerRadius
	for _, ox := range [2]float32{-r, r} {
		for _, oz := range [2]float32{-r, r} {
			cx := int(math.Floor(float64(x + ox)))
			cz := int(math.Floor(float64(z + oz)))
			if cx < 0 || cz < 0 || cx >= mapW || cz >= mapH {
				return true
			}
			if cellAt(cx, cz) == '#' {
				return true
			}
		}
	}
	return false
}

func cellAt(x, z int) byte {
	rows := strings.Split(strings.TrimSpace(mapStr), "\n")
	if z < 0 || z >= len(rows) {
		return '#'
	}
	if x < 0 || x >= len(rows[z]) {
		return '#'
	}
	return rows[z][x]
}

func drawCube(uModel, uTint int32, model mat4, tint vec3) {
	gl.UniformMatrix4fv(uModel, 1, false, &model[0])
	gl.Uniform3f(uTint, tint.x, tint.y, tint.z)
	gl.DrawElements(gl.TRIANGLES, int32(len(cubeIndices)), gl.UNSIGNED_INT, nil)
}

// ─── Shaders ─────────────────────────────────────────────────────────────────

const vsSrc = `#version 330 core
layout(location=0) in vec3 aPos;
layout(location=1) in vec3 aNormal;
layout(location=2) in vec3 aColor;
uniform mat4 uModel;
uniform mat4 uView;
uniform mat4 uProj;
out vec3 vNormalWS;
out vec3 vColor;
void main() {
    gl_Position = uProj * uView * uModel * vec4(aPos, 1.0);
    // We only translate / uniform-scale, so the upper 3x3 of uModel
    // is enough to transform the normal into world space.
    vNormalWS = (uModel * vec4(aNormal, 0.0)).xyz;
    vColor = aColor;
}` + "\x00"

const fsSrc = `#version 330 core
in vec3 vNormalWS;
in vec3 vColor;
uniform vec3 uTint;
uniform vec3 uSunDir;  // pre-normalised, points from surface to sun
out vec4 fragColor;
void main() {
    vec3 N = normalize(vNormalWS);
    float diffuse = max(dot(N, uSunDir), 0.0);
    float lit = 0.30 + 0.70 * diffuse;  // ambient + diffuse
    fragColor = vec4(vColor * uTint * lit, 1.0);
}` + "\x00"

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
