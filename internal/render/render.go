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

// ─── 5×7 pixel font ──────────────────────────────────────────────────────────
//
// Each glyph is a 7-row × 5-column bitmap stored as 7 strings of 'X'/'.'.
// Glyph fills its (w × h) bounding box by drawing one Rect per lit pixel,
// pixel size = min(w/5, h/7).  Unknown runes render nothing.
//
// Letters are uppercase only.  Digits are full.  Add more glyphs as games
// need them; this is a deliberate small set, not a full font.

var font5x7 = map[rune][7]string{
	' ': {".....", ".....", ".....", ".....", ".....", ".....", "....."},
	'-': {".....", ".....", ".....", ".XXX.", ".....", ".....", "....."},
	'!': {"..X..", "..X..", "..X..", "..X..", "..X..", ".....", "..X.."},
	':': {".....", "..X..", "..X..", ".....", "..X..", "..X..", "....."},
	'.': {".....", ".....", ".....", ".....", ".....", "..X..", "..X.."},
	'0': {".XXX.", "X...X", "X..XX", "X.X.X", "XX..X", "X...X", ".XXX."},
	'1': {"..X..", ".XX..", "..X..", "..X..", "..X..", "..X..", ".XXX."},
	'2': {".XXX.", "X...X", "....X", "...X.", "..X..", ".X...", "XXXXX"},
	'3': {"XXXXX", "...X.", "..X..", "...X.", "....X", "X...X", ".XXX."},
	'4': {"...X.", "..XX.", ".X.X.", "X..X.", "XXXXX", "...X.", "...X."},
	'5': {"XXXXX", "X....", "XXXX.", "....X", "....X", "X...X", ".XXX."},
	'6': {".XXX.", "X....", "X....", "XXXX.", "X...X", "X...X", ".XXX."},
	'7': {"XXXXX", "....X", "...X.", "..X..", ".X...", "X....", "X...."},
	'8': {".XXX.", "X...X", "X...X", ".XXX.", "X...X", "X...X", ".XXX."},
	'9': {".XXX.", "X...X", "X...X", ".XXXX", "....X", "....X", ".XXX."},
	'A': {".XXX.", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"},
	'B': {"XXXX.", "X...X", "X...X", "XXXX.", "X...X", "X...X", "XXXX."},
	'C': {".XXXX", "X....", "X....", "X....", "X....", "X....", ".XXXX"},
	'D': {"XXXX.", "X...X", "X...X", "X...X", "X...X", "X...X", "XXXX."},
	'E': {"XXXXX", "X....", "X....", "XXXX.", "X....", "X....", "XXXXX"},
	'F': {"XXXXX", "X....", "X....", "XXXX.", "X....", "X....", "X...."},
	'G': {".XXXX", "X....", "X....", "X..XX", "X...X", "X...X", ".XXXX"},
	'H': {"X...X", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"},
	'I': {"XXXXX", "..X..", "..X..", "..X..", "..X..", "..X..", "XXXXX"},
	'J': {"..XXX", "....X", "....X", "....X", "....X", "X...X", ".XXX."},
	'K': {"X...X", "X..X.", "X.X..", "XX...", "X.X..", "X..X.", "X...X"},
	'L': {"X....", "X....", "X....", "X....", "X....", "X....", "XXXXX"},
	'M': {"X...X", "XX.XX", "X.X.X", "X.X.X", "X...X", "X...X", "X...X"},
	'N': {"X...X", "XX..X", "X.X.X", "X..XX", "X...X", "X...X", "X...X"},
	'O': {".XXX.", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."},
	'P': {"XXXX.", "X...X", "X...X", "XXXX.", "X....", "X....", "X...."},
	'Q': {".XXX.", "X...X", "X...X", "X...X", "X.X.X", "X..X.", ".XX.X"},
	'R': {"XXXX.", "X...X", "X...X", "XXXX.", "X.X..", "X..X.", "X...X"},
	'S': {".XXXX", "X....", "X....", ".XXX.", "....X", "....X", "XXXX."},
	'T': {"XXXXX", "..X..", "..X..", "..X..", "..X..", "..X..", "..X.."},
	'U': {"X...X", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."},
	'V': {"X...X", "X...X", "X...X", "X...X", "X...X", ".X.X.", "..X.."},
	'W': {"X...X", "X...X", "X...X", "X.X.X", "X.X.X", "XX.XX", "X...X"},
	'X': {"X...X", "X...X", ".X.X.", "..X..", ".X.X.", "X...X", "X...X"},
	'Y': {"X...X", "X...X", ".X.X.", "..X..", "..X..", "..X..", "..X.."},
	'Z': {"XXXXX", "....X", "...X.", "..X..", ".X...", "X....", "XXXXX"},
}

// Glyph draws a single character at (x, y) inside a (w × h) box.  The pixel
// size is the largest square that fits both the 5-column and 7-row
// constraints, so the glyph keeps its aspect ratio inside the box.  The t
// parameter is ignored (kept for signature compatibility).
func (r *Renderer) Glyph(x, y, w, h, _ float32, c rune, cr, cg, cb float32) {
	rows, ok := font5x7[c]
	if !ok {
		return
	}
	pix := w / 5
	if h/7 < pix {
		pix = h / 7
	}
	for ry, row := range rows {
		for cx, ch := range row {
			if ch == 'X' {
				r.Rect(x+float32(cx)*pix, y+float32(ry)*pix, pix, pix, cr, cg, cb)
			}
		}
	}
}

// Text draws a string left-to-right starting at (x, y), each glyph sized
// (w × h).  A small inter-glyph gap is added so adjacent letters don't
// touch.  The t parameter is preserved for signature compatibility but
// ignored by the 5×7 renderer.
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
