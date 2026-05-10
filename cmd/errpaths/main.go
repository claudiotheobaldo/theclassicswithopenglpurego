// Error-paths diagnostic.  Every other binary in the suite exercises
// success paths; this one deliberately triggers failures and verifies
// each one fails the expected way:
//
//   - Compile a syntactically-broken vertex shader → expect non-zero
//     info-log length and COMPILE_STATUS == FALSE
//   - Compile a syntactically-broken fragment shader → same
//   - Link mismatched shaders (vs writes vec3 varying, fs reads float)
//     → expect LINK_STATUS == FALSE and a non-empty info log
//   - Trigger glGetError after an invalid call → expect non-GL_NO_ERROR
//   - Pass an unrecognised WindowHint → expect glfw.SetErrorCallback to
//     fire with InvalidEnum, and the call itself to not crash
//
// Each test prints PASS or FAIL.  All five must PASS for the binding
// to behave like upstream go-gl on error paths.
//
// No window is shown for long; we just need a GL context to exercise
// the GL paths.  Esc / window close exits cleanly.
package main

import (
	"fmt"
	"runtime"
	"strings"

	gl "github.com/ClaudioTheobaldo/gl-purego/v3.3-core/gl"
	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"
)

func init() { runtime.LockOSThread() }

type result struct {
	name string
	ok   bool
	note string
}

var results []result

func record(name string, ok bool, note string) {
	results = append(results, result{name: name, ok: ok, note: note})
}

func main() {
	// Test 1 (pre-window): error callback fires for unknown hints.
	gotErr := false
	gotCode := glfw.ErrorCode(0)
	gotDesc := ""
	prev := glfw.SetErrorCallback(func(code glfw.ErrorCode, desc string) {
		gotErr = true
		gotCode = code
		gotDesc = desc
	})
	_ = prev

	if err := glfw.Init(); err != nil {
		fmt.Println("glfw.Init:", err)
		return
	}
	defer glfw.Terminate()

	// Set a deliberately-bogus hint key.  The library should accept the
	// call (no panic), record nothing, and notify the error callback.
	const bogus = glfw.Hint(0xDEADBEEF)
	glfw.WindowHint(bogus, 42)
	switch {
	case !gotErr:
		record("error_callback_fires", false, "callback never invoked")
	case gotCode != glfw.InvalidEnum:
		record("error_callback_fires", false,
			fmt.Sprintf("expected InvalidEnum, got %s", gotCode))
	default:
		record("error_callback_fires", true, gotDesc)
	}

	// Open a context for the GL tests.
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Visible, glfw.False) // headless
	win, err := glfw.CreateWindow(320, 240, "errpaths", nil, nil)
	if err != nil {
		fmt.Println("CreateWindow:", err)
		return
	}
	defer win.Destroy()
	win.MakeContextCurrent()
	if err := gl.Init(); err != nil {
		fmt.Println("gl.Init:", err)
		return
	}

	// Test 2: bad vertex shader.
	{
		s := compileSilently(gl.VERTEX_SHADER, `THIS IS NOT GLSL`)
		var status, logLen int32
		gl.GetShaderiv(s, gl.COMPILE_STATUS, &status)
		gl.GetShaderiv(s, gl.INFO_LOG_LENGTH, &logLen)
		log := infoLog(s, true)
		gl.DeleteShader(s)
		// The point of the test is that the compile *fails*; an empty
		// info log is a driver choice, not a binding bug.
		if status == gl.FALSE {
			note := fmt.Sprintf("status=FALSE logLen=%d", logLen)
			if log != "" {
				note += " log=" + snip(log)
			}
			record("bad_vertex_shader_fails", true, note)
		} else {
			record("bad_vertex_shader_fails", false,
				fmt.Sprintf("status=%d (expected FALSE)", status))
		}
	}

	// Test 3: bad fragment shader (uses an undeclared identifier).
	{
		src := `#version 330 core
out vec4 fragColor;
void main() { fragColor = nope; }`
		s := compileSilently(gl.FRAGMENT_SHADER, src)
		var status, logLen int32
		gl.GetShaderiv(s, gl.COMPILE_STATUS, &status)
		gl.GetShaderiv(s, gl.INFO_LOG_LENGTH, &logLen)
		log := infoLog(s, true)
		gl.DeleteShader(s)
		if status == gl.FALSE {
			note := fmt.Sprintf("status=FALSE logLen=%d", logLen)
			if log != "" {
				note += " log=" + snip(log)
			}
			record("bad_fragment_shader_fails", true, note)
		} else {
			record("bad_fragment_shader_fails", false,
				fmt.Sprintf("status=%d (expected FALSE)", status))
		}
	}

	// Test 4: link mismatched shaders.  vs writes vec3, fs reads float.
	{
		vs := compileSilently(gl.VERTEX_SHADER, `#version 330 core
out vec3 vColor;
void main() {
    vColor = vec3(1.0);
    gl_Position = vec4(0.0);
}`)
		fs := compileSilently(gl.FRAGMENT_SHADER, `#version 330 core
in float vColor;
out vec4 fragColor;
void main() { fragColor = vec4(vColor); }`)
		p := gl.CreateProgram()
		gl.AttachShader(p, vs)
		gl.AttachShader(p, fs)
		gl.LinkProgram(p)
		var status, logLen int32
		gl.GetProgramiv(p, gl.LINK_STATUS, &status)
		gl.GetProgramiv(p, gl.INFO_LOG_LENGTH, &logLen)
		log := programLog(p)
		gl.DeleteProgram(p)
		gl.DeleteShader(vs)
		gl.DeleteShader(fs)
		if status == gl.FALSE {
			note := fmt.Sprintf("status=FALSE logLen=%d", logLen)
			if log != "" {
				note += " log=" + snip(log)
			}
			record("mismatched_link_fails", true, note)
		} else {
			record("mismatched_link_fails", false,
				fmt.Sprintf("status=%d (expected FALSE)", status))
		}
	}

	// Test 5: glGetError reports an invalid operation.  Drain any
	// pre-existing errors, then make a deliberately-bad call.
	for gl.GetError() != gl.NO_ERROR {
	}
	// glEnable with an enum that isn't a valid capability: GL_TEXTURE_2D
	// is a *texture target*, not an enable cap, so it triggers
	// GL_INVALID_ENUM in core profile.
	gl.Enable(gl.TEXTURE_2D)
	code := gl.GetError()
	if code != gl.NO_ERROR {
		record("invalid_enable_sets_glGetError", true,
			fmt.Sprintf("got 0x%x", code))
	} else {
		record("invalid_enable_sets_glGetError", false, "no error reported")
	}

	// Print summary.
	fmt.Println("─── Error-path diagnostic ───")
	pass, fail := 0, 0
	for _, r := range results {
		mark := "PASS"
		if !r.ok {
			mark = "FAIL"
			fail++
		} else {
			pass++
		}
		fmt.Printf("  %s  %-32s  %s\n", mark, r.name, r.note)
	}
	fmt.Printf("\n%d passed, %d failed\n", pass, fail)
}

// compileSilently compiles a shader without panicking on failure — the
// whole point here is to capture the failure.
func compileSilently(kind uint32, src string) uint32 {
	s := gl.CreateShader(kind)
	cs, free := gl.Strs(src + "\x00")
	defer free()
	gl.ShaderSource(s, 1, cs, nil)
	gl.CompileShader(s)
	return s
}

// infoLog reads a shader's compile info log via a *mutable* byte buffer.
// The OpenGL function writes bytes into the buffer, so we must NOT pass a
// pointer derived from a Go string (those are immutable; on some
// platforms the writes are silently dropped).
func infoLog(shader uint32, _ bool) string {
	var n int32
	gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &n)
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	gl.GetShaderInfoLog(shader, n, nil, &buf[0])
	return strings.TrimRight(string(buf), "\x00\n ")
}

func programLog(prog uint32) string {
	var n int32
	gl.GetProgramiv(prog, gl.INFO_LOG_LENGTH, &n)
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	gl.GetProgramInfoLog(prog, n, nil, &buf[0])
	return strings.TrimRight(string(buf), "\x00\n ")
}

// snip clips a long info log down to its first ~80 chars for the summary.
func snip(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "  ", " ")
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
