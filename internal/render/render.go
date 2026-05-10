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
	"math"
	"strings"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
)

// Renderer owns the shaders and VAOs.  One per GL context.
//
// Three pipelines coexist:
//   - rect: uniform-driven, axis-aligned filled quads (used by Rect, glyphs)
//   - line: pixel-space vertices uploaded per-call, used for arbitrary
//     line/polyline drawing (Line, PolygonStroke).
//   - tex:  textured quad with foreground/background colour mix, used by
//     DrawTexture for grid bitmaps and similar.
//
// Begin() sets the rect program; other calls swap programs as needed.
type Renderer struct {
	// rect pipeline
	rectProg          uint32
	rectVAO, rectVBO  uint32
	rectURect         int32
	rectUColor        int32
	rectUViewport     int32

	// line pipeline
	lineProg          uint32
	lineVAO, lineVBO  uint32
	lineCap           int // VBO capacity in float32s
	lineUColor        int32
	lineUViewport     int32

	// texture pipeline (shares the unit-quad VBO with rect)
	texProg       uint32
	texURect      int32
	texUViewport  int32
	texUFG, texUBG int32
	texUSampler   int32

	// indexed-palette texture pipeline
	idxProg       uint32
	idxURect      int32
	idxUViewport  int32
	idxUSampler   int32
	idxUPalette   int32

	// RGBA texture pipeline (used by image viewer, FBO sampling)
	rgbaProg      uint32
	rgbaURect     int32
	rgbaUViewport int32
	rgbaUSampler  int32
	rgbaUTint     int32
	rgbaUFlip     int32

	viewportW, viewportH int
	current              int // 0 = rect, 1 = line, 2 = tex
}

// New compiles the shaders and creates VAOs.  Call after gl.Init().
func New() *Renderer {
	r := &Renderer{}

	// ── rect pipeline ──
	r.rectProg = compileProgram(rectVS, fsSrc)
	r.rectURect = gl.GetUniformLocation(r.rectProg, gl.Str("uRect\x00"))
	r.rectUColor = gl.GetUniformLocation(r.rectProg, gl.Str("uColor\x00"))
	r.rectUViewport = gl.GetUniformLocation(r.rectProg, gl.Str("uViewport\x00"))

	quad := []float32{0, 0, 1, 0, 0, 1, 1, 1}
	gl.GenVertexArrays(1, &r.rectVAO)
	gl.BindVertexArray(r.rectVAO)
	gl.GenBuffers(1, &r.rectVBO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.rectVBO)
	gl.BufferData(gl.ARRAY_BUFFER, len(quad)*4, gl.Ptr(quad), gl.STATIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 0, 0)

	// ── line pipeline ──
	r.lineProg = compileProgram(lineVS, fsSrc)
	r.lineUColor = gl.GetUniformLocation(r.lineProg, gl.Str("uColor\x00"))
	r.lineUViewport = gl.GetUniformLocation(r.lineProg, gl.Str("uViewport\x00"))

	gl.GenVertexArrays(1, &r.lineVAO)
	gl.BindVertexArray(r.lineVAO)
	gl.GenBuffers(1, &r.lineVBO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.lineVBO)
	r.lineCap = 256 // grows on demand
	gl.BufferData(gl.ARRAY_BUFFER, r.lineCap*4, nil, gl.DYNAMIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 0, 0)

	// ── texture pipeline ── (re-uses the rect unit-quad VBO via the
	// rect VAO; we just bind a different program before drawing.)
	r.texProg = compileProgram(texVS, texFS)
	r.texURect = gl.GetUniformLocation(r.texProg, gl.Str("uRect\x00"))
	r.texUViewport = gl.GetUniformLocation(r.texProg, gl.Str("uViewport\x00"))
	r.texUFG = gl.GetUniformLocation(r.texProg, gl.Str("uFG\x00"))
	r.texUBG = gl.GetUniformLocation(r.texProg, gl.Str("uBG\x00"))
	r.texUSampler = gl.GetUniformLocation(r.texProg, gl.Str("uTex\x00"))

	// ── indexed-palette texture pipeline ──
	r.idxProg = compileProgram(texVS, idxFS)
	r.idxURect = gl.GetUniformLocation(r.idxProg, gl.Str("uRect\x00"))
	r.idxUViewport = gl.GetUniformLocation(r.idxProg, gl.Str("uViewport\x00"))
	r.idxUSampler = gl.GetUniformLocation(r.idxProg, gl.Str("uTex\x00"))
	r.idxUPalette = gl.GetUniformLocation(r.idxProg, gl.Str("uPalette\x00"))

	// ── RGBA texture pipeline ──
	r.rgbaProg = compileProgram(texVS, rgbaFS)
	r.rgbaURect = gl.GetUniformLocation(r.rgbaProg, gl.Str("uRect\x00"))
	r.rgbaUViewport = gl.GetUniformLocation(r.rgbaProg, gl.Str("uViewport\x00"))
	r.rgbaUSampler = gl.GetUniformLocation(r.rgbaProg, gl.Str("uTex\x00"))
	r.rgbaUTint = gl.GetUniformLocation(r.rgbaProg, gl.Str("uTint\x00"))
	r.rgbaUFlip = gl.GetUniformLocation(r.rgbaProg, gl.Str("uFlip\x00"))

	return r
}

// Destroy releases GL resources.
func (r *Renderer) Destroy() {
	gl.DeleteBuffers(1, &r.rectVBO)
	gl.DeleteVertexArrays(1, &r.rectVAO)
	gl.DeleteProgram(r.rectProg)
	gl.DeleteBuffers(1, &r.lineVBO)
	gl.DeleteVertexArrays(1, &r.lineVAO)
	gl.DeleteProgram(r.lineProg)
	gl.DeleteProgram(r.texProg)
	gl.DeleteProgram(r.idxProg)
	gl.DeleteProgram(r.rgbaProg)
}

// Begin records the viewport size and binds the rect pipeline.  Call once
// per frame before any draw calls.
func (r *Renderer) Begin(viewportW, viewportH int) {
	r.viewportW, r.viewportH = viewportW, viewportH
	r.bindRect()
}

func (r *Renderer) bindRect() {
	if r.current != 0 {
		gl.UseProgram(r.rectProg)
		gl.BindVertexArray(r.rectVAO)
		r.current = 0
	} else {
		gl.UseProgram(r.rectProg)
		gl.BindVertexArray(r.rectVAO)
	}
	gl.Uniform2f(r.rectUViewport, float32(r.viewportW), float32(r.viewportH))
}

func (r *Renderer) bindLine() {
	if r.current != 1 {
		gl.UseProgram(r.lineProg)
		gl.BindVertexArray(r.lineVAO)
		r.current = 1
	}
	gl.Uniform2f(r.lineUViewport, float32(r.viewportW), float32(r.viewportH))
}

// Rect draws a filled axis-aligned rectangle at (x, y) with size (w, h) and
// the given RGB colour.  Coordinates are pixels, origin top-left.
func (r *Renderer) Rect(x, y, w, h, cr, cg, cb float32) {
	if r.current != 0 {
		r.bindRect()
	}
	gl.Uniform4f(r.rectURect, x, y, w, h)
	gl.Uniform3f(r.rectUColor, cr, cg, cb)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
}

// ─── Line + polyline ────────────────────────────────────────────────────────
//
// Lines render as thin rotated quads (4-vertex triangle strips) with vertices
// computed CPU-side and uploaded per call.  Core-profile GL caps glLineWidth
// at 1, so quads are the only portable path for thicker strokes.

// Line draws a thickness-px-wide line from (x1, y1) to (x2, y2).
func (r *Renderer) Line(x1, y1, x2, y2, thickness, cr, cg, cb float32) {
	dx, dy := x2-x1, y2-y1
	length := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if length == 0 {
		return
	}
	// perpendicular unit vector × half-thickness
	px, py := -dy/length*thickness*0.5, dx/length*thickness*0.5
	verts := [8]float32{
		x1 + px, y1 + py,
		x1 - px, y1 - py,
		x2 + px, y2 + py,
		x2 - px, y2 - py,
	}
	r.bindLine()
	gl.BindBuffer(gl.ARRAY_BUFFER, r.lineVBO)
	if r.lineCap < 8 {
		r.lineCap = 8
		gl.BufferData(gl.ARRAY_BUFFER, r.lineCap*4, gl.Ptr(&verts[0]), gl.DYNAMIC_DRAW)
	} else {
		gl.BufferSubData(gl.ARRAY_BUFFER, 0, 8*4, gl.Ptr(&verts[0]))
	}
	gl.Uniform3f(r.lineUColor, cr, cg, cb)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
}

// ─── Textures ───────────────────────────────────────────────────────────────
//
// Textures are single-channel 8-bit, intended for grid/bitmap data.  Upload
// a w*h byte slice; sample as fg/bg blend in DrawTexture.  Filtering is
// nearest, so each texel renders as a sharp block at any draw size — exactly
// what cellular automata and pixel-art grids want.

// Texture is a GPU-side texture, single-channel R8 by default; RGBA8 if
// created via NewTextureRGBA.  rgba and fbAttached are used by the
// renderer to pick the right sampling shader and Y-flip direction.
type Texture struct {
	id         uint32
	w, h       int32
	rgba       bool
	fbAttached bool
}

// NewTexture allocates a w x h GL_R8 texture.  Use Upload to populate it.
func (r *Renderer) NewTexture(w, h int) *Texture {
	t := &Texture{w: int32(w), h: int32(h)}
	gl.GenTextures(1, &t.id)
	gl.BindTexture(gl.TEXTURE_2D, t.id)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.R8, t.w, t.h, 0, gl.RED, gl.UNSIGNED_BYTE, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	return t
}

// Upload replaces the texture's pixel data.  data must contain exactly w*h
// bytes (single-channel).
func (t *Texture) Upload(data []byte) {
	if len(data) != int(t.w)*int(t.h) {
		panic("Texture.Upload: wrong data length")
	}
	gl.BindTexture(gl.TEXTURE_2D, t.id)
	// One-byte rows; default unpack alignment is 4 which would pad odd
	// widths.  Set 1 so any width works.
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, t.w, t.h, gl.RED, gl.UNSIGNED_BYTE, gl.Ptr(data))
}

// Destroy releases the texture.
func (t *Texture) Destroy() { gl.DeleteTextures(1, &t.id) }

// Size returns the texture's dimensions in pixels.
func (t *Texture) Size() (w, h int) { return int(t.w), int(t.h) }

// ID returns the underlying GL texture ID.  Useful when other code needs
// to interact with the texture directly (e.g. binding it as an FBO
// attachment).
func (t *Texture) ID() uint32 { return t.id }

// ─── RGBA textures ──────────────────────────────────────────────────────────
//
// Four-byte-per-pixel textures for image data, screenshots, and
// framebuffer-attached colour buffers.  Sampled with linear filtering
// (good for photo-style content) — switch to nearest with SetNearest if
// you need crisp pixel art.

// NewTextureRGBA allocates a w x h GL_RGBA8 texture with linear filtering.
func (r *Renderer) NewTextureRGBA(w, h int) *Texture {
	t := &Texture{w: int32(w), h: int32(h)}
	gl.GenTextures(1, &t.id)
	gl.BindTexture(gl.TEXTURE_2D, t.id)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, t.w, t.h, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	t.rgba = true
	return t
}

// UploadRGBA replaces the texture's pixel data with w*h*4 bytes of RGBA.
func (t *Texture) UploadRGBA(data []byte) {
	if len(data) != int(t.w)*int(t.h)*4 {
		panic("Texture.UploadRGBA: wrong data length")
	}
	gl.BindTexture(gl.TEXTURE_2D, t.id)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 4)
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, t.w, t.h, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(data))
}

// SetNearest forces nearest-neighbour filtering (sharp pixels at any
// draw size).  Default for RGBA textures is linear.
func (t *Texture) SetNearest() {
	gl.BindTexture(gl.TEXTURE_2D, t.id)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
}

// ─── Framebuffer (offscreen render target) ──────────────────────────────────

// Framebuffer is an offscreen render target backed by an RGBA colour
// texture.  Use Bind() to switch all subsequent draws to it, Unbind() to
// return to the default window framebuffer.  Sample the result via
// DrawRGBATexture.
type Framebuffer struct {
	id  uint32
	tex *Texture
}

// NewFramebuffer creates a w x h FBO with a single RGBA colour attachment.
func (r *Renderer) NewFramebuffer(w, h int) *Framebuffer {
	fb := &Framebuffer{tex: r.NewTextureRGBA(w, h)}
	gl.GenFramebuffers(1, &fb.id)
	gl.BindFramebuffer(gl.FRAMEBUFFER, fb.id)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, fb.tex.id, 0)
	if gl.CheckFramebufferStatus(gl.FRAMEBUFFER) != gl.FRAMEBUFFER_COMPLETE {
		panic("Framebuffer not complete")
	}
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	fb.tex.fbAttached = true
	return fb
}

// Bind directs subsequent rendering at this framebuffer and resets the
// GL viewport to the framebuffer's dimensions.  Don't forget to call
// Renderer.Begin again with matching dimensions before issuing draws.
func (fb *Framebuffer) Bind() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, fb.id)
	gl.Viewport(0, 0, fb.tex.w, fb.tex.h)
}

// Unbind restores the default framebuffer.  Caller must reset gl.Viewport
// to the window's framebuffer size before drawing again.
func (fb *Framebuffer) Unbind() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}

// Texture returns the colour-attachment texture; sample it via
// DrawRGBATexture or copy from it via glReadPixels.
func (fb *Framebuffer) Texture() *Texture { return fb.tex }

// Destroy releases the FBO and its colour texture.
func (fb *Framebuffer) Destroy() {
	gl.DeleteFramebuffers(1, &fb.id)
	fb.tex.Destroy()
}

// DrawRGBATexture samples an RGBA texture (typically an FBO's colour
// attachment) into a (w x h) rect.  Optional tint multiplies the sampled
// colour; pass [4]float32{1,1,1,1} for a passthrough copy.
func (r *Renderer) DrawRGBATexture(t *Texture, x, y, w, h float32, tint [4]float32) {
	gl.UseProgram(r.rgbaProg)
	gl.BindVertexArray(r.rectVAO)
	r.current = 4
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, t.id)
	gl.Uniform1i(r.rgbaUSampler, 0)
	gl.Uniform2f(r.rgbaUViewport, float32(r.viewportW), float32(r.viewportH))
	gl.Uniform4f(r.rgbaURect, x, y, w, h)
	gl.Uniform4f(r.rgbaUTint, tint[0], tint[1], tint[2], tint[3])
	// Y-flip toggle: FBOs render bottom-up, raw image data is usually
	// top-down.  Pass via a uniform so the same shader serves both.
	flip := float32(0)
	if t.fbAttached {
		flip = 1
	}
	gl.Uniform1f(r.rgbaUFlip, flip)
	// Enable alpha blending for tinted overlay use.
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
	gl.Disable(gl.BLEND)
}

// DrawTexture renders the texture into a (w x h) rect at (x, y), with each
// texel mixed between bg and fg by its 0..255 value (0 = bg, 255 = fg).
func (r *Renderer) DrawTexture(t *Texture, x, y, w, h float32, fg, bg [3]float32) {
	gl.UseProgram(r.texProg)
	gl.BindVertexArray(r.rectVAO) // shares the unit-quad
	r.current = 2
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, t.id)
	gl.Uniform1i(r.texUSampler, 0)
	gl.Uniform2f(r.texUViewport, float32(r.viewportW), float32(r.viewportH))
	gl.Uniform4f(r.texURect, x, y, w, h)
	gl.Uniform3f(r.texUFG, fg[0], fg[1], fg[2])
	gl.Uniform3f(r.texUBG, bg[0], bg[1], bg[2])
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
}

// DrawTextureIndexed renders the texture as a palette lookup: each texel's
// 0..15 value indexes into the 16-entry palette.  Use this when cells store
// arbitrary indices rather than scalar intensity (paint apps, tile maps,
// etc.).  Indices >= 16 wrap.
func (r *Renderer) DrawTextureIndexed(t *Texture, x, y, w, h float32, palette [16][3]float32) {
	gl.UseProgram(r.idxProg)
	gl.BindVertexArray(r.rectVAO)
	r.current = 3
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, t.id)
	gl.Uniform1i(r.idxUSampler, 0)
	gl.Uniform2f(r.idxUViewport, float32(r.viewportW), float32(r.viewportH))
	gl.Uniform4f(r.idxURect, x, y, w, h)
	// Upload as a flat array of 48 floats — Uniform3fv on a vec3[16].
	flat := [48]float32{}
	for i := 0; i < 16; i++ {
		flat[i*3+0] = palette[i][0]
		flat[i*3+1] = palette[i][1]
		flat[i*3+2] = palette[i][2]
	}
	gl.Uniform3fv(r.idxUPalette, 16, &flat[0])
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
}

// PolygonStroke draws line segments through the given vertices.  When
// closed=true it also draws a closing segment from the last vertex back to
// the first.  All segments share the same thickness and colour.
func (r *Renderer) PolygonStroke(verts [][2]float32, thickness, cr, cg, cb float32, closed bool) {
	if len(verts) < 2 {
		return
	}
	for i := 0; i < len(verts)-1; i++ {
		r.Line(verts[i][0], verts[i][1], verts[i+1][0], verts[i+1][1], thickness, cr, cg, cb)
	}
	if closed {
		last := verts[len(verts)-1]
		first := verts[0]
		r.Line(last[0], last[1], first[0], first[1], thickness, cr, cg, cb)
	}
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

// glyphGap returns the inter-glyph gap for a per-glyph width of w.  Scaled
// with the font size so small text stays tight and big text doesn't run
// together.  Floor at 2 px so 0.5x-ratio pixel art still has visible
// separation.
func glyphGap(w float32) float32 {
	g := w * 0.4
	if g < 2 {
		g = 2
	}
	return g
}

// Text draws a string left-to-right starting at (x, y), each glyph sized
// (w × h).  Inter-glyph gap scales with w so layouts stay balanced across
// font sizes.  The t parameter is preserved for signature compatibility
// but ignored by the 5×7 renderer.
func (r *Renderer) Text(x, y, w, h, t float32, s string, cr, cg, cb float32) {
	gap := glyphGap(w)
	for i, c := range s {
		r.Glyph(x+float32(i)*(w+gap), y, w, h, t, c, cr, cg, cb)
	}
}

// Number is Text(fmt.Sprintf("%d", n), …).
func (r *Renderer) Number(x, y, w, h, t float32, n int, cr, cg, cb float32) {
	r.Text(x, y, w, h, t, fmt.Sprintf("%d", n), cr, cg, cb)
}

// TextWidth returns the rendered width of s when drawn with per-glyph width
// w, matching the same gap Text uses internally.  Use this to lay out HUD
// elements without having to duplicate the layout maths in callers.
func TextWidth(s string, w float32) float32 {
	if len(s) == 0 {
		return 0
	}
	return float32(len(s))*w + float32(len(s)-1)*glyphGap(w)
}

// NumberWidth is TextWidth(fmt.Sprintf("%d", n), w).
func NumberWidth(n int, w float32) float32 {
	return TextWidth(fmt.Sprintf("%d", n), w)
}

// HasGlyph reports whether the renderer can draw r.  The font is uppercase
// only and covers digits and a small punctuation set; everything else
// renders blank.  Diagnostic / log displays use this to substitute a
// fallback rune so unknown characters don't disappear.
func HasGlyph(r rune) bool {
	_, ok := font5x7[r]
	return ok
}

// Sanitize returns s upper-cased with any unknown runes replaced by '?'.
// Useful when echoing unconstrained input (filenames, key names, etc) on
// screen with the 5x7 font.
func Sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range strings.ToUpper(s) {
		if HasGlyph(r) {
			out = append(out, r)
		} else {
			out = append(out, '?')
		}
	}
	return string(out)
}

// ─── Shader plumbing ────────────────────────────────────────────────────────

const rectVS = `#version 330 core
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

const lineVS = `#version 330 core
layout(location=0) in vec2 aPos; // pixel-space, origin top-left
uniform vec2 uViewport;
void main() {
    vec2 ndc = vec2(
         aPos.x / uViewport.x * 2.0 - 1.0,
        -(aPos.y / uViewport.y * 2.0 - 1.0)
    );
    gl_Position = vec4(ndc, 0.0, 1.0);
}` + "\x00"

const fsSrc = `#version 330 core
uniform vec3 uColor;
out vec4 fragColor;
void main() { fragColor = vec4(uColor, 1.0); }` + "\x00"

const texVS = `#version 330 core
layout(location=0) in vec2 aQuad;
uniform vec4 uRect;
uniform vec2 uViewport;
out vec2 vUV;
void main() {
    vec2 px = uRect.xy + aQuad * uRect.zw;
    vec2 ndc = vec2(
         px.x / uViewport.x * 2.0 - 1.0,
        -(px.y / uViewport.y * 2.0 - 1.0)
    );
    gl_Position = vec4(ndc, 0.0, 1.0);
    vUV = aQuad;
}` + "\x00"

const texFS = `#version 330 core
in vec2 vUV;
uniform sampler2D uTex;
uniform vec3 uFG;
uniform vec3 uBG;
out vec4 fragColor;
void main() {
    float v = texture(uTex, vUV).r;
    fragColor = vec4(mix(uBG, uFG, v), 1.0);
}` + "\x00"

const idxFS = `#version 330 core
in vec2 vUV;
uniform sampler2D uTex;
uniform vec3 uPalette[16];
out vec4 fragColor;
void main() {
    int idx = int(texture(uTex, vUV).r * 255.0 + 0.5) & 15;
    fragColor = vec4(uPalette[idx], 1.0);
}` + "\x00"

const rgbaFS = `#version 330 core
in vec2 vUV;
uniform sampler2D uTex;
uniform vec4 uTint;
uniform float uFlip;  // 1.0 to flip Y (FBO sampling), 0.0 for raw images
out vec4 fragColor;
void main() {
    vec2 uv = vec2(vUV.x, mix(vUV.y, 1.0 - vUV.y, uFlip));
    fragColor = texture(uTex, uv) * uTint;
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
