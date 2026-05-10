// Soak test — long-running stress harness for glfw-purego and gl-purego.
//
// Most classics binaries run for minutes; bug #2 (the GetMonitors
// callback leak) manifested only after ~2000 frames.  This program runs
// indefinitely (or for a fixed duration), pounds on the API paths most
// likely to leak, and periodically logs:
//
//   - runtime.MemStats:     HeapAlloc, NumGC, goroutines
//   - Win32 GUI resources:  GDI + USER handle counts (process-local
//     totals; both have a 10k per-process ceiling)
//   - glfw cadence:         iteration count, monitors-currently-attached
//
// What's exercised every frame:
//
//   - GetMonitors()                       (bug #2 regression net)
//   - GetJoystickAxes/Buttons/Hats        on every connected stick
//   - PollEvents and SwapBuffers
//   - PostEmptyEvent from a background goroutine
//
// Periodic batches every ~10 s simulate an interactive app's churn
// without needing a human at the keyboard:
//
//   - 100 CreateStandardCursor + Destroy round-trips
//   - 50 CreateCursor (image) + Destroy round-trips
//   - 20 NewTextureRGBA upload-and-destroy round-trips
//
// If any counter trends upward over hours of running, that's a leak.
//
// Flags
//   -duration   stop after this much wall time (0 = forever)
//   -interval   how often to log stats (default 30s)
//   -minimized  start the window iconified to stay out of the way
//
// Esc closes the window early.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"os"
	"runtime"
	"time"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"
)

var (
	flagDuration  = flag.Duration("duration", 0, "stop after this much wall time (0 = forever)")
	flagInterval  = flag.Duration("interval", 30*time.Second, "stats logging interval")
	flagMinimized = flag.Bool("minimized", false, "start the window iconified")
)

const (
	winW = 360
	winH = 200

	cursorBatchSize  = 100
	customBatchSize  = 50
	textureBatchSize = 20

	batchEveryFrames = 600 // about 10 s at 60 FPS
)

func init() { runtime.LockOSThread() }

func main() {
	flag.Parse()

	if err := glfw.Init(); err != nil {
		fmt.Println("glfw.Init:", err)
		os.Exit(1)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Resizable, glfw.False)

	win, err := glfw.CreateWindow(winW, winH, "Soak Test", nil, nil)
	if err != nil {
		fmt.Println("CreateWindow:", err)
		os.Exit(1)
	}
	win.MakeContextCurrent()
	glfw.SwapInterval(1)
	if err := gl.Init(); err != nil {
		fmt.Println("gl.Init:", err)
		os.Exit(1)
	}

	if *flagMinimized {
		win.Iconify()
	}

	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if key == glfw.KeyEscape && action == glfw.Press {
			win.SetShouldClose(true)
		}
	})

	// Background ticker tests PostEmptyEvent from a non-main goroutine
	// over the entire soak duration — the agent flagged this as
	// untested for long-running sessions.
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				glfw.PostEmptyEvent()
			}
		}
	}()
	defer close(stop)

	start := time.Now()
	lastReport := start
	iterations := 0
	cursorBatches := 0
	customBatches := 0
	textureBatches := 0
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	fmt.Printf("Soak test started.  duration=%v  interval=%v\n",
		dispDuration(*flagDuration), *flagInterval)
	fmt.Println("Press Esc in the window to stop early.")
	fmt.Println()
	printHeader()
	printStats(start, iterations, cursorBatches+customBatches, textureBatches)

	for !win.ShouldClose() {
		iterations++

		// Hot-loop polling: every frame, hit the API surfaces most likely
		// to leak callbacks / memory / handles.
		_ = glfw.GetMonitors()
		for j := glfw.Joystick1; j <= glfw.JoystickLast; j++ {
			if glfw.JoystickPresent(j) {
				_ = glfw.GetJoystickAxes(j)
				_ = glfw.GetJoystickButtons(j)
				_ = glfw.GetJoystickHats(j)
			}
		}

		// Periodic batches.  Spread across separate frames so we don't
		// stall the swap loop with a 170-allocation burst.
		switch iterations % batchEveryFrames {
		case 100:
			churnStandardCursors()
			cursorBatches++
		case 200:
			churnCustomCursors()
			customBatches++
		case 300:
			churnTextures(rng)
			textureBatches++
		}

		gl.ClearColor(0.05, 0.06, 0.10, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		win.SwapBuffers()
		glfw.PollEvents()

		if time.Since(lastReport) >= *flagInterval {
			printStats(start, iterations, cursorBatches+customBatches, textureBatches)
			lastReport = time.Now()
		}
		if *flagDuration > 0 && time.Since(start) >= *flagDuration {
			break
		}
	}

	fmt.Println()
	fmt.Println("=== FINAL ===")
	printStats(start, iterations, cursorBatches+customBatches, textureBatches)
}

// ─── Churn helpers ──────────────────────────────────────────────────────────

func churnStandardCursors() {
	for i := 0; i < cursorBatchSize; i++ {
		c := glfw.CreateStandardCursor(glfw.ArrowCursor)
		if c != nil {
			c.Destroy()
		}
	}
}

func churnCustomCursors() {
	img := makeTinyCursorImage()
	for i := 0; i < customBatchSize; i++ {
		c := glfw.CreateCursor(img, 8, 8)
		if c != nil {
			c.Destroy()
		}
	}
}

func churnTextures(rng *rand.Rand) {
	// Use sizes that aren't multiples of 4 to also catch any
	// pack-alignment regressions.
	const w, h = 65, 47
	pix := make([]byte, w*h*4)
	for i := range pix {
		pix[i] = byte(rng.Intn(256))
	}
	for i := 0; i < textureBatchSize; i++ {
		var id uint32
		gl.GenTextures(1, &id)
		gl.BindTexture(gl.TEXTURE_2D, id)
		gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, w, h, 0,
			gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pix))
		gl.DeleteTextures(1, &id)
	}
}

// makeTinyCursorImage returns a cheap 16×16 RGBA cursor sprite.  Same
// content every call, but the underlying CreateCursor still allocates
// a fresh OS handle, which is what we're testing.
func makeTinyCursorImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if x == 8 || y == 8 {
				img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	return img
}

// ─── Stats ───────────────────────────────────────────────────────────────────

func printHeader() {
	fmt.Printf("%-10s  %-10s  %-9s  %-7s  %-6s  %-7s  %-9s  %s\n",
		"ELAPSED", "ITERS", "HEAP_KB", "NUM_GC", "GOROS", "CURSORS",
		"TEXTURES", "GDI/USER")
}

func printStats(start time.Time, iters, cursorBatches, textureBatches int) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	gdi, usr := guiResources()
	fmt.Printf("%-10s  %-10d  %-9d  %-7d  %-6d  %-7d  %-9d  %d/%d\n",
		dispElapsed(time.Since(start)),
		iters,
		ms.HeapAlloc/1024,
		ms.NumGC,
		runtime.NumGoroutine(),
		cursorBatches*cursorBatchSize, // approximate; mixes std + custom
		textureBatches*textureBatchSize,
		gdi, usr,
	)
}

// dispElapsed prints a duration in a compact form: 12s, 1m05s, 1h05m, 2d4h.
func dispElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		return fmt.Sprintf("%dm%02ds", m, s)
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		return fmt.Sprintf("%dh%02dm", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%02dh", days, h)
	}
}

func dispDuration(d time.Duration) string {
	if d == 0 {
		return "forever"
	}
	return d.String()
}
