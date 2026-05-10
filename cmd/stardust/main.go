// Stardust — instanced particle storm.
//
// Up to 50,000 particles drawn in a single glDrawArraysInstanced call.
// Each particle ships per-instance data (position, colour, size, alpha)
// to the GPU each frame; the vertex shader expands a unit quad to a
// world-space rect and the fragment shader samples two procedural
// textures (a soft glow + a sparkle cross).  Output is additively
// blended for a glowing, never-quite-opaque look.
//
// First program in the suite to exercise:
//   - glDrawArraysInstanced + glVertexAttribDivisor (per-instance attribs)
//   - Two simultaneous samplers (TEXTURE0 + TEXTURE1) — every other
//     consumer always bound to unit 0
//   - Streamed per-instance VBO updated every frame (50k * 7 floats)
//   - Additive blending (gl.BLEND with SRC_ALPHA + ONE)
//   - F12 screenshot via the new internal/screenshot helper
//
// Controls
//   Mouse drag (left)  : continuous emission at the cursor
//   Click              : burst of 400 particles
//   Space              : auto-fire firework bursts
//   F12                : save a PNG screenshot to the working dir
//   F11                : fullscreen
//   Esc                : quit
package main

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"time"
	"unsafe"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/screenshot"
	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/winutil"
)

const (
	winW, winH    = 1000, 750
	maxParticles  = 50000
	burstSize     = 400
	autoFireRate  = 0.45 // s between auto-fired bursts
	gravity       = 320.0
	floatsPerInst = 7 // px, py, r, g, b, size, alpha
)

func init() { runtime.LockOSThread() }

type particle struct {
	px, py    float32
	vx, vy   float32
	r, g, b  float32
	size     float32
	life     float32
	maxLife  float32
}

var (
	parts    [maxParticles]particle
	count    int
	autofire bool
	rng      = rand.New(rand.NewSource(time.Now().UnixNano()))
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

	win, err := glfw.CreateWindow(winW, winH, "Stardust", nil, nil)
	if err != nil {
		panic(err)
	}
	win.MakeContextCurrent()
	glfw.SwapInterval(1)
	if err := gl.Init(); err != nil {
		panic(err)
	}

	prog := compileProgram(vsSrc, fsSrc)
	defer gl.DeleteProgram(prog)

	// Uniform locations.
	uViewport := gl.GetUniformLocation(prog, gl.Str("uViewport\x00"))
	uGlow := gl.GetUniformLocation(prog, gl.Str("uGlow\x00"))
	uSpark := gl.GetUniformLocation(prog, gl.Str("uSpark\x00"))

	// VAO.
	var vao uint32
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)

	// Static unit-quad VBO (4 verts, 2 floats each), at attrib 0.
	quad := []float32{-1, -1, 1, -1, -1, 1, 1, 1}
	var quadVBO uint32
	gl.GenBuffers(1, &quadVBO)
	gl.BindBuffer(gl.ARRAY_BUFFER, quadVBO)
	gl.BufferData(gl.ARRAY_BUFFER, len(quad)*4, gl.Ptr(quad), gl.STATIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)

	// Dynamic per-instance VBO at attribs 1..4.  Layout per instance:
	//   1: vec2 aInstPos       (px, py)        offset  0
	//   2: vec3 aInstColor     (r, g, b)       offset  8
	//   3: float aInstSize                     offset 20
	//   4: float aInstAlpha                    offset 24
	// Stride = 28 bytes.
	var instVBO uint32
	gl.GenBuffers(1, &instVBO)
	gl.BindBuffer(gl.ARRAY_BUFFER, instVBO)
	gl.BufferData(gl.ARRAY_BUFFER, maxParticles*floatsPerInst*4, nil, gl.STREAM_DRAW)
	stride := int32(floatsPerInst * 4)
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointerWithOffset(1, 2, gl.FLOAT, false, stride, 0)
	gl.EnableVertexAttribArray(2)
	gl.VertexAttribPointerWithOffset(2, 3, gl.FLOAT, false, stride, 2*4)
	gl.EnableVertexAttribArray(3)
	gl.VertexAttribPointerWithOffset(3, 1, gl.FLOAT, false, stride, 5*4)
	gl.EnableVertexAttribArray(4)
	gl.VertexAttribPointerWithOffset(4, 1, gl.FLOAT, false, stride, 6*4)
	// Per-instance: advance attribute once per instance, not per vertex.
	gl.VertexAttribDivisor(1, 1)
	gl.VertexAttribDivisor(2, 1)
	gl.VertexAttribDivisor(3, 1)
	gl.VertexAttribDivisor(4, 1)

	// Procedural textures bound to TEXTURE0 (glow) and TEXTURE1 (sparkle).
	glowTex := makeRadialGlow(64)
	sparkTex := makeSparkle(64)
	defer gl.DeleteTextures(1, &glowTex)
	defer gl.DeleteTextures(1, &sparkTex)

	// Input.
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press {
			return
		}
		switch key {
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		case glfw.KeyF11:
			winutil.ToggleFullscreen(win)
		case glfw.KeyF12:
			fbW, fbH := win.GetFramebufferSize()
			if name, err := screenshot.Save("stardust", fbW, fbH); err != nil {
				fmt.Println("screenshot:", err)
			} else {
				fmt.Println("saved", name)
			}
			if err := screenshot.CopyToClipboard(fbW, fbH); err != nil {
				fmt.Println("clipboard:", err)
			} else {
				fmt.Println("copied to clipboard")
			}
		case glfw.KeySpace:
			autofire = !autofire
		}
	})
	win.SetMouseButtonCallback(func(_ *glfw.Window, btn glfw.MouseButton, action glfw.Action, _ glfw.ModifierKey) {
		if btn == glfw.MouseButtonLeft && action == glfw.Press {
			mx, my := win.GetCursorPos()
			burst(float32(mx), float32(my))
		}
	})

	// Pre-allocated CPU staging buffer for instance data.
	staging := make([]float32, maxParticles*floatsPerInst)

	last := time.Now()
	autoTimer := 0.0
	for !win.ShouldClose() {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		if dt > 0.05 {
			dt = 0.05
		}
		last = now

		// Continuous spray under held mouse.
		if win.GetMouseButton(glfw.MouseButtonLeft) == glfw.Press {
			mx, my := win.GetCursorPos()
			spray(float32(mx), float32(my), 30, dt)
		}
		if autofire {
			autoTimer += dt
			if autoTimer > autoFireRate {
				autoTimer = 0
				burst(rng.Float32()*winW, 100+rng.Float32()*300)
			}
		}

		updateParticles(dt)
		nLive := packInstances(staging)

		gl.ClearColor(0.02, 0.02, 0.04, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		gl.Enable(gl.BLEND)
		gl.BlendFunc(gl.SRC_ALPHA, gl.ONE) // additive — particles glow
		gl.UseProgram(prog)
		gl.BindVertexArray(vao)
		gl.Uniform2f(uViewport, float32(winW), float32(winH))

		gl.ActiveTexture(gl.TEXTURE0)
		gl.BindTexture(gl.TEXTURE_2D, glowTex)
		gl.Uniform1i(uGlow, 0)
		gl.ActiveTexture(gl.TEXTURE1)
		gl.BindTexture(gl.TEXTURE_2D, sparkTex)
		gl.Uniform1i(uSpark, 1)

		if nLive > 0 {
			gl.BindBuffer(gl.ARRAY_BUFFER, instVBO)
			gl.BufferSubData(gl.ARRAY_BUFFER, 0, nLive*floatsPerInst*4,
				gl.Ptr(staging))
			gl.DrawArraysInstanced(gl.TRIANGLE_STRIP, 0, 4, int32(nLive))
		}
		gl.Disable(gl.BLEND)

		win.SwapBuffers()
		glfw.PollEvents()
	}
}

// ─── Particle pool ───────────────────────────────────────────────────────────
//
// The pool is a fixed-size array; we keep `count` packed at the front and
// swap-in the last live particle when one dies.  No allocations after init.

func updateParticles(dt float64) {
	dts := float32(dt)
	g := float32(gravity)
	i := 0
	for i < count {
		p := &parts[i]
		p.vy += g * dts
		p.px += p.vx * dts
		p.py += p.vy * dts
		p.life -= dts
		if p.life <= 0 || p.py > winH+50 || p.px < -50 || p.px > winW+50 {
			parts[i] = parts[count-1]
			count--
			continue
		}
		i++
	}
}

// packInstances writes the live particle pool into the staging slice in
// instance-VBO order.  Returns the number of live particles written.
func packInstances(buf []float32) int {
	for i := 0; i < count; i++ {
		p := &parts[i]
		alpha := p.life / p.maxLife
		if alpha > 1 {
			alpha = 1
		}
		j := i * floatsPerInst
		buf[j+0] = p.px
		buf[j+1] = p.py
		buf[j+2] = p.r
		buf[j+3] = p.g
		buf[j+4] = p.b
		buf[j+5] = p.size
		buf[j+6] = alpha
	}
	return count
}

func spawn(px, py, vx, vy, r, g, b, size, life float32) {
	if count >= maxParticles {
		return
	}
	parts[count] = particle{
		px: px, py: py, vx: vx, vy: vy,
		r: r, g: g, b: b, size: size,
		life: life, maxLife: life,
	}
	count++
}

func burst(cx, cy float32) {
	hue := rng.Float32()
	for i := 0; i < burstSize; i++ {
		angle := rng.Float32() * 2 * math.Pi
		speed := 80 + rng.Float32()*240
		rr, gg, bb := hsvToRGB(hue+rng.Float32()*0.18, 0.85, 1.0)
		size := 2 + rng.Float32()*3
		life := 0.8 + rng.Float32()*1.0
		spawn(cx, cy,
			float32(math.Cos(float64(angle)))*speed,
			float32(math.Sin(float64(angle)))*speed,
			rr, gg, bb, size, life)
	}
}

// spray emits N particles per second from (cx, cy) with low velocity for
// continuous mouse-trail effects.
func spray(cx, cy float32, perSec int, dt float64) {
	n := int(float64(perSec) * dt)
	if n < 1 && rng.Float64() < float64(perSec)*dt {
		n = 1
	}
	for i := 0; i < n; i++ {
		angle := rng.Float32() * 2 * math.Pi
		speed := 40 + rng.Float32()*100
		rr, gg, bb := hsvToRGB(rng.Float32(), 0.7, 1.0)
		spawn(cx+rng.Float32()*4-2, cy+rng.Float32()*4-2,
			float32(math.Cos(float64(angle)))*speed,
			float32(math.Sin(float64(angle)))*speed*0.5-40,
			rr, gg, bb, 2.5, 0.9+rng.Float32()*0.6)
	}
}

// ─── Procedural textures ─────────────────────────────────────────────────────

// makeRadialGlow creates an n×n RGBA texture with a smooth radial alpha
// falloff: 1.0 at the centre, 0.0 at the edge, gauss-shaped between.
func makeRadialGlow(n int) uint32 {
	pix := make([]byte, n*n*4)
	cx := float64(n) / 2
	cy := float64(n) / 2
	maxR := float64(n) / 2
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d := math.Sqrt(dx*dx + dy*dy)
			t := 1.0 - d/maxR
			if t < 0 {
				t = 0
			}
			t = t * t // sharper centre
			a := byte(t * 255)
			i := (y*n + x) * 4
			pix[i+0] = 255
			pix[i+1] = 255
			pix[i+2] = 255
			pix[i+3] = a
		}
	}
	return uploadRGBA(n, n, pix)
}

// makeSparkle creates an n×n RGBA texture with a 4-pointed star: bright
// horizontal and vertical streaks meeting at the centre.
func makeSparkle(n int) uint32 {
	pix := make([]byte, n*n*4)
	cx := float64(n) / 2
	cy := float64(n) / 2
	maxR := float64(n) / 2
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx := math.Abs(float64(x)+0.5-cx) / maxR
			dy := math.Abs(float64(y)+0.5-cy) / maxR
			// Streak strength: bright along the centre row/column.
			h := math.Max(0, 1-dy*8) * math.Max(0, 1-dx*1.2)
			v := math.Max(0, 1-dx*8) * math.Max(0, 1-dy*1.2)
			s := h + v
			if s > 1 {
				s = 1
			}
			a := byte(s * 255)
			i := (y*n + x) * 4
			pix[i+0] = 255
			pix[i+1] = 255
			pix[i+2] = 255
			pix[i+3] = a
		}
	}
	return uploadRGBA(n, n, pix)
}

func uploadRGBA(w, h int, pix []byte) uint32 {
	var id uint32
	gl.GenTextures(1, &id)
	gl.BindTexture(gl.TEXTURE_2D, id)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(w), int32(h), 0,
		gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pix))
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	return id
}

// ─── Shaders ─────────────────────────────────────────────────────────────────

const vsSrc = `#version 330 core
layout(location=0) in vec2 aQuad;       // -1..+1 unit quad
layout(location=1) in vec2 aInstPos;     // pixel-space centre
layout(location=2) in vec3 aInstColor;
layout(location=3) in float aInstSize;
layout(location=4) in float aInstAlpha;
uniform vec2 uViewport;
out vec2 vUV;
out vec3 vColor;
out float vAlpha;
void main() {
    vec2 px = aInstPos + aQuad * aInstSize * 8.0;  // particle quad spans size*8
    vec2 ndc = vec2(
         px.x / uViewport.x * 2.0 - 1.0,
        -(px.y / uViewport.y * 2.0 - 1.0)
    );
    gl_Position = vec4(ndc, 0.0, 1.0);
    vUV = aQuad * 0.5 + 0.5;
    vColor = aInstColor;
    vAlpha = aInstAlpha;
}` + "\x00"

const fsSrc = `#version 330 core
in vec2 vUV;
in vec3 vColor;
in float vAlpha;
uniform sampler2D uGlow;
uniform sampler2D uSpark;
out vec4 fragColor;
void main() {
    float glow = texture(uGlow, vUV).a;
    float spark = texture(uSpark, vUV).a;
    float intensity = glow * 0.7 + spark * 0.3;
    fragColor = vec4(vColor * intensity, intensity * vAlpha);
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
		buf := make([]byte, n)
		gl.GetProgramInfoLog(p, n, nil, &buf[0])
		panic("link: " + string(buf))
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
		buf := make([]byte, n)
		gl.GetShaderInfoLog(s, n, nil, &buf[0])
		panic("compile: " + string(buf))
	}
	return s
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func hsvToRGB(h, s, v float32) (float32, float32, float32) {
	h = float32(math.Mod(float64(h), 1.0)) * 6
	c := v * s
	x := c * (1 - float32(math.Abs(math.Mod(float64(h), 2)-1)))
	m := v - c
	var r, g, b float32
	switch {
	case h < 1:
		r, g, b = c, x, 0
	case h < 2:
		r, g, b = x, c, 0
	case h < 3:
		r, g, b = 0, c, x
	case h < 4:
		r, g, b = 0, x, c
	case h < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}

// keep the unsafe import alive in case future glReadPixels-y tweaks
// need raw pointer math
var _ = unsafe.Sizeof(particle{})
