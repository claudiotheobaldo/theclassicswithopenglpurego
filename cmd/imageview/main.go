// Image viewer — drag-and-drop test that consumes file paths.
//
// Drop one or more PNG / JPEG files onto the window from your file
// manager.  The first one is loaded into a GL_RGBA texture and shown
// fitted to the window's aspect.  If multiple files are dropped, arrow
// keys cycle through them.
//
// First program in the suite to:
//   - Actually consume drag-and-drop paths via SetDropCallback
//     (eventtape only logged them)
//   - Decode images via stdlib image/png + image/jpeg
//   - Use the renderer's RGBA texture path with real photo-style content
//
// Controls
//   Drop files     : load the dropped images
//   Left / Right   : previous / next image
//   F11            : fullscreen
//   Esc            : quit
package main

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"

	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/render"
	"github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego/internal/winutil"
)

const startW, startH = 900, 700

type loaded struct {
	path string
	tex  *render.Texture
	w, h int
}

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

	win, err := glfw.CreateWindow(startW, startH, "Image Viewer — drop files", nil, nil)
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

	fbW, fbH := win.GetFramebufferSize()
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		fbW, fbH = w, h
		gl.Viewport(0, 0, int32(w), int32(h))
	})

	var images []*loaded
	current := -1

	loadOne := func(path string) {
		l, err := loadImage(r, path)
		if err != nil {
			fmt.Printf("load %q: %v\n", path, err)
			return
		}
		images = append(images, l)
		current = len(images) - 1
		win.SetTitle(fmt.Sprintf("Image Viewer — %s [%d/%d]", filepath.Base(path), current+1, len(images)))
	}

	win.SetDropCallback(func(_ *glfw.Window, paths []string) {
		for _, p := range paths {
			loadOne(p)
		}
	})

	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
		switch key {
		case glfw.KeyEscape:
			win.SetShouldClose(true)
		case glfw.KeyLeft:
			if len(images) > 0 {
				current = (current - 1 + len(images)) % len(images)
				win.SetTitle(fmt.Sprintf("Image Viewer — %s [%d/%d]",
					filepath.Base(images[current].path), current+1, len(images)))
			}
		case glfw.KeyRight:
			if len(images) > 0 {
				current = (current + 1) % len(images)
				win.SetTitle(fmt.Sprintf("Image Viewer — %s [%d/%d]",
					filepath.Base(images[current].path), current+1, len(images)))
			}
		case glfw.KeyF11:
			if action == glfw.Press {
				winutil.ToggleFullscreen(win)
			}
		}
	})

	for !win.ShouldClose() {
		gl.ClearColor(0.06, 0.07, 0.10, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)

		r.Begin(fbW, fbH)
		if current >= 0 && current < len(images) {
			img := images[current]
			x, y, w, h := winutil.LetterboxRect(fbW, fbH, img.w, img.h)
			r.DrawRGBATexture(img.tex, float32(x), float32(y), float32(w), float32(h), [4]float32{1, 1, 1, 1})
		} else {
			// Hint text when nothing's loaded.
			msg := "DROP IMAGE FILES HERE"
			const w, h float32 = 16, 22
			tw := render.TextWidth(msg, w)
			r.Text(float32(fbW)/2-tw/2, float32(fbH)/2-h/2, w, h, 0, msg, 0.6, 0.7, 0.85)
		}

		win.SwapBuffers()
		glfw.PollEvents()
	}
}

// loadImage reads a PNG/JPEG (anything image.Decode supports via the
// blank-imported decoders) and uploads it as an RGBA8 texture.  The
// returned texture is configured for nearest-neighbour filtering so
// pixel art stays crisp when stretched.
func loadImage(r *render.Renderer, path string) (*loaded, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Re-encode as RGBA bytes if necessary so the buffer matches what GL
	// expects.  *image.RGBA already has the right layout.
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(bounds)
		draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)
	}

	tex := r.NewTextureRGBA(w, h)
	tex.UploadRGBA(rgba.Pix)
	return &loaded{path: path, tex: tex, w: w, h: h}, nil
}
