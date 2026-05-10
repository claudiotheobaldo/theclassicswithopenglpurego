// Package screenshot reads the current default framebuffer with
// glReadPixels and writes it as a PNG.  Used by F12 hotkeys across
// games.  First entry to exercise gl-purego's pixel-readback path.
package screenshot

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
)

// Save reads the current framebuffer (assumes default fb is bound) and
// writes a PNG named "<prefix>-YYYYMMDD-HHMMSS.png" to the working
// directory.  Returns the chosen filename on success.
//
// glReadPixels reads bottom-up while image.RGBA is top-down, so we
// reverse the rows on the way out.
func Save(prefix string, w, h int) (string, error) {
	if w <= 0 || h <= 0 {
		return "", fmt.Errorf("screenshot: bad size %dx%d", w, h)
	}
	pixels := make([]byte, w*h*4)
	gl.PixelStorei(gl.PACK_ALIGNMENT, 1)
	gl.ReadPixels(0, 0, int32(w), int32(h), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rowBytes := w * 4
	for y := 0; y < h; y++ {
		src := pixels[(h-1-y)*rowBytes : (h-y)*rowBytes]
		copy(img.Pix[y*rowBytes:(y+1)*rowBytes], src)
	}

	ts := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("%s-%s.png", prefix, ts)
	f, err := os.Create(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return name, nil
}
