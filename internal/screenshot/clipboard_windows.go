//go:build windows

package screenshot

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
)

// CopyToClipboard reads the current default framebuffer and places the
// result on the Windows clipboard as a CF_DIB image.  The clipboard
// payload is BITMAPINFOHEADER + 32-bit BGRA pixels stored bottom-up —
// the natural layout glReadPixels gives us once R and B are swapped,
// so no Y-flip is required.
//
// After successful SetClipboardData the clipboard owns the global
// memory; we don't free it on success.
func CopyToClipboard(w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("CopyToClipboard: bad size %dx%d", w, h)
	}

	pixels := make([]byte, w*h*4)
	gl.PixelStorei(gl.PACK_ALIGNMENT, 1)
	gl.ReadPixels(0, 0, int32(w), int32(h), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pixels))

	// RGBA -> BGRA (the two middle channels stay).
	for i := 0; i < len(pixels); i += 4 {
		pixels[i], pixels[i+2] = pixels[i+2], pixels[i]
	}

	headerSize := uintptr(unsafe.Sizeof(bitmapInfoHeader{}))
	pixelSize := uintptr(len(pixels))
	total := headerSize + pixelSize

	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, total)
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc failed")
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return fmt.Errorf("GlobalLock failed")
	}

	*(*bitmapInfoHeader)(unsafe.Pointer(ptr)) = bitmapInfoHeader{
		biSize:        uint32(headerSize),
		biWidth:       int32(w),
		biHeight:      int32(h), // positive = bottom-up, matching glReadPixels
		biPlanes:      1,
		biBitCount:    32,
		biCompression: 0, // BI_RGB
		biSizeImage:   uint32(pixelSize),
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(ptr+headerSize)), pixelSize)
	copy(dst, pixels)
	procGlobalUnlock.Call(hMem)

	// Open / replace / set / close.  hwnd=0 associates with the calling
	// task, which is fine for a single-process app.
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		procGlobalFree.Call(hMem)
		return fmt.Errorf("OpenClipboard failed")
	}
	procEmptyClipboard.Call()
	if r, _, _ := procSetClipboardData.Call(cfDIB, hMem); r == 0 {
		procCloseClipboard.Call()
		procGlobalFree.Call(hMem)
		return fmt.Errorf("SetClipboardData failed")
	}
	procCloseClipboard.Call()
	return nil
}

// ─── Win32 plumbing ─────────────────────────────────────────────────────────

const (
	cfDIB        = 8
	gmemMoveable = 0x0002
)

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procCloseClipboard   = user32.NewProc("CloseClipboard")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
)
