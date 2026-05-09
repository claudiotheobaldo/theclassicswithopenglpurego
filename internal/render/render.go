// Package render is the tiny shared 2D drawing toolkit used by every game in
// the suite.  It owns one shader and one unit-quad VAO and exposes a single
// primitive — Rect — plus a 7-segment digit/letter helper layered on top.
//
// Games create a Renderer once after gl.Init(), call Begin() at the top of
// every frame to bind GL state and announce the current viewport, then issue
// any number of Rect / Number / Text calls.  Coordinates are in pixels with
// the origin at the top-left, matching how level/UI code naturally thinks.
package render

import (
	"fmt"
	"strings"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
)

// Renderer owns the shader and quad VAO.  One per GL context.
type Renderer struct {
	prog      uint32
	vao, vbo  uint32
	uRect     int32
	uColor    int32
	uViewport int32
}

// New compiles the shader and creates the VAO.  Call after gl.Init().
func New() *Renderer {
	r := &Renderer{}
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

// Destroy releases GL resources.
func (r *Renderer) Destroy() {
	gl.DeleteBuffers(1, &r.vbo)
	gl.DeleteVertexArrays(1, &r.vao)
	gl.DeleteProgram(r.prog)
}

// Begin binds the program/VAO and sets the viewport size in pixels.  Call
// once per frame before any Rect / Number / Text calls.
func (r *Renderer) Begin(viewportW, viewportH int) {
	gl.UseProgram(r.prog)
	gl.BindVertexArray(r.vao)
	gl.Uniform2f(r.uViewport, float32(viewportW), float32(viewportH))
}

// Rect draws a filled axis-aligned rectangle at (x, y) with size (w, h) and
// the given RGB colour.  Coordinates are pixels, origin top-left.
func (r *Renderer) Rect(x, y, w, h, cr, cg, cb float32) {
	gl.Uniform4f(r.uRect, x, y, w, h)
	gl.Uniform3f(r.uColor, cr, cg, cb)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
}

// ─── 7-segment digit / letter helpers ────────────────────────────────────────
//
// Layout:
//      aaaa
//     f    b
//     f    b
//      gggg
//     e    c
//     e    c
//      dddd
//
// Each glyph is encoded as a 7-bit mask, MSB = segment a.

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
	'A': 0b1110111,
	'C': 0b1001110,
	'E': 0b1001111,
	'G': 0b1011110,
	'H': 0b0110111,
	'I': 0b0110000,
	'L': 0b0001110,
	'N': 0b1110110, // approximate
	'O': 0b1111110,
	'P': 0b1100111,
	'R': 0b1100111, // approximate
	'S': 0b1011011,
	'U': 0b0111110,
	'W': 0b0101110, // approximate
	'-': 0b0000001,
	' ': 0,
}

// Glyph draws a single 7-segment character at (x, y) sized (w × h) with
// segment thickness t.  Unknown runes draw nothing.
func (r *Renderer) Glyph(x, y, w, h, t float32, c rune, cr, cg, cb float32) {
	bits, ok := glyphs[c]
	if !ok {
		return
	}
	seg := func(i int, sx, sy, sw, sh float32) {
		if bits&(1<<(6-i)) != 0 {
			r.Rect(sx, sy, sw, sh, cr, cg, cb)
		}
	}
	seg(0, x, y, w, t)                     // a
	seg(1, x+w-t, y, t, h/2)               // b
	seg(2, x+w-t, y+h/2, t, h/2)           // c
	seg(3, x, y+h-t, w, t)                 // d
	seg(4, x, y+h/2, t, h/2)               // e
	seg(5, x, y, t, h/2)                   // f
	seg(6, x, y+h/2-t/2, w, t)             // g
}

// Text draws a string left-to-right starting at (x, y), each glyph sized
// (w × h) with t-px-thick segments and a fixed gap between glyphs.
func (r *Renderer) Text(x, y, w, h, t float32, s string, cr, cg, cb float32) {
	const gap = 10
	for i, c := range s {
		r.Glyph(x+float32(i)*(w+gap), y, w, h, t, c, cr, cg, cb)
	}
}

// Number is Text(fmt.Sprintf("%d", n), …).
func (r *Renderer) Number(x, y, w, h, t float32, n int, cr, cg, cb float32) {
	r.Text(x, y, w, h, t, fmt.Sprintf("%d", n), cr, cg, cb)
}

// ─── Shader plumbing ────────────────────────────────────────────────────────

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
