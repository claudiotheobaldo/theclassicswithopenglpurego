// Mandelbrot zoomer — fragment-shader-driven fractal explorer.
//
// First program in the suite to:
//   - Update GLSL uniforms every frame in response to user input
//   - Use the scroll wheel as primary input (Pong/Snake/Asteroids/Breakout
//     ignore it; eventtape only logged it)
//   - Toggle between windowed and fullscreen via SetMonitor (no other
//     consumer exercised the SetMonitor round-trip with a real monitor)
//
// Controls
//   Mouse scroll      : zoom in/out around the cursor
//   Left mouse drag   : pan
//   Mouse wheel click : reset view
//   + / -             : more / fewer iterations (sharper / smoother edges)
//   Space             : cycle palette colours
//   F11               : toggle fullscreen
//   Esc               : quit
package main

import (
	"fmt"
	"runtime"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/winutil"
)

const (
	startW, startH = 900, 700
)

var (
	centerX  = -0.5
	centerY  = 0.0
	scale    = 3.0 // visible width in complex plane
	iterMax  = 220
	palShift = 0.0
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

	win, err := glfw.CreateWindow(startW, startH, "Mandelbrot", nil, nil)
	if err != nil {
		panic(err)
	}
	win.MakeContextCurrent()
	glfw.SwapInterval(1)
	if err := gl.Init(); err != nil {
		panic(err)
	}

	prog := compileProgram()
	defer gl.DeleteProgram(prog)

	var vao, vbo uint32
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	quad := []float32{0, 0, 1, 0, 0, 1, 1, 1}
	gl.BufferData(gl.ARRAY_BUFFER, len(quad)*4, gl.Ptr(quad), gl.STATIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 0, 0)

	uCenter := gl.GetUniformLocation(prog, gl.Str("uCenter\x00"))
	uScale := gl.GetUniformLocation(prog, gl.Str("uScale\x00"))
	uAspect := gl.GetUniformLocation(prog, gl.Str("uAspect\x00"))
	uIter := gl.GetUniformLocation(prog, gl.Str("uIter\x00"))
	uShift := gl.GetUniformLocation(prog, gl.Str("uShift\x00"))

	// Track window/framebuffer size so the aspect uniform stays correct.
	fbW, fbH := win.GetFramebufferSize()
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		fbW, fbH = w, h
		gl.Viewport(0, 0, int32(w), int32(h))
	})

	win.SetScrollCallback(func(_ *glfw.Window, dx, dy float64) {
		// Zoom around the cursor: keep the world point under the cursor
		// fixed in pixel space across the zoom step.
		mx, my := win.GetCursorPos()
		wx, wy := pixelToWorld(mx, my, fbW, fbH)
		factor := 0.85
		if dy < 0 {
			factor = 1.0 / 0.85
		}
		scale *= factor
		// shift center so (wx, wy) stays under the cursor
		nwx, nwy := pixelToWorld(mx, my, fbW, fbH)
		centerX += wx - nwx
		centerY += wy - nwy
	})

	dragging := false
	lastMX, lastMY := 0.0, 0.0
	win.SetMouseButtonCallback(func(_ *glfw.Window, btn glfw.MouseButton, action glfw.Action, _ glfw.ModifierKey) {
		if btn == glfw.MouseButtonLeft {
			if action == glfw.Press {
				dragging = true
				lastMX, lastMY = win.GetCursorPos()
			} else if action == glfw.Release {
				dragging = false
			}
		}
		if btn == glfw.MouseButtonMiddle && action == glfw.Press {
			centerX, centerY, scale = -0.5, 0.0, 3.0
			iterMax = 220
		}
	})
	win.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		if dragging {
			dx := x - lastMX
			dy := y - lastMY
			ax, ay := worldStep(fbW, fbH)
			centerX -= dx * ax
			centerY -= dy * ay
			lastMX, lastMY = x, y
		}
	})

	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
		switch key {
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		case glfw.KeyEqual, glfw.KeyKPAdd:
			iterMax += 40
			if iterMax > 4000 {
				iterMax = 4000
			}
		case glfw.KeyMinus, glfw.KeyKPSubtract:
			iterMax -= 40
			if iterMax < 40 {
				iterMax = 40
			}
		case glfw.KeySpace:
			palShift += 0.1
		case glfw.KeyF11:
			if action == glfw.Press {
				winutil.ToggleFullscreen(win)
			}
		}
	})

	for !win.ShouldClose() {
		gl.Clear(gl.COLOR_BUFFER_BIT)

		gl.UseProgram(prog)
		gl.BindVertexArray(vao)
		gl.Uniform2f(uCenter, float32(centerX), float32(centerY))
		gl.Uniform1f(uScale, float32(scale))
		gl.Uniform2f(uAspect, 1.0, float32(fbH)/float32(fbW))
		gl.Uniform1i(uIter, int32(iterMax))
		gl.Uniform1f(uShift, float32(palShift))
		gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)

		win.SwapBuffers()
		glfw.PollEvents()
	}
	_ = time.Now() // keep time imported for future use; unused for now
}

// pixelToWorld converts (px, py) in framebuffer-pixel coords to world-space
// coords on the complex plane using the current centre / scale / aspect.
func pixelToWorld(px, py float64, fbW, fbH int) (float64, float64) {
	uvX := px / float64(fbW)
	uvY := 1.0 - py/float64(fbH) // GL Y is flipped relative to mouse Y
	aspectY := float64(fbH) / float64(fbW)
	wx := centerX + (uvX-0.5)*scale
	wy := centerY + (uvY-0.5)*scale*aspectY
	return wx, wy
}

// worldStep returns how many world units one pixel of mouse delta covers
// in (x, y).  Used for click-drag panning.
func worldStep(fbW, fbH int) (float64, float64) {
	return scale / float64(fbW), -scale * (float64(fbH) / float64(fbW)) / float64(fbH)
}

// ─── Shader plumbing ─────────────────────────────────────────────────────────

const vsSrc = `#version 330 core
layout(location=0) in vec2 aQuad;
out vec2 vUV;
void main() {
    gl_Position = vec4(aQuad * 2.0 - 1.0, 0, 1);
    vUV = aQuad;
}` + "\x00"

const fsSrc = `#version 330 core
in vec2 vUV;
uniform vec2 uCenter;
uniform float uScale;
uniform vec2 uAspect;   // (1, fbH/fbW)
uniform int uIter;
uniform float uShift;
out vec4 fragColor;
void main() {
    vec2 c = uCenter + (vUV - 0.5) * uScale * uAspect;
    vec2 z = vec2(0.0);
    int i;
    float r2 = 0.0;
    for (i = 0; i < uIter; i++) {
        z = vec2(z.x*z.x - z.y*z.y, 2.0 * z.x * z.y) + c;
        r2 = dot(z, z);
        if (r2 > 4.0) break;
    }
    if (i >= uIter) {
        fragColor = vec4(0.0, 0.0, 0.0, 1.0);
        return;
    }
    // Smooth iteration count for banding-free colouring.
    float smooth_i = float(i) - log2(log(r2) * 0.5);
    float t = smooth_i / float(uIter) + uShift;
    vec3 col = 0.5 + 0.5 * cos(6.28318 * (t + vec3(0.0, 0.33, 0.67)));
    fragColor = vec4(col, 1.0);
}` + "\x00"

func compileProgram() uint32 {
	v := compileShader(gl.VERTEX_SHADER, vsSrc)
	f := compileShader(gl.FRAGMENT_SHADER, fsSrc)
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
		panic(fmt.Sprintf("compile: %s", string(buf)))
	}
	return s
}
