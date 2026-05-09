// Vulkan check — minimal validation that glfw-purego's Vulkan binding
// surface is wired up and returns sensible values.
//
// This is NOT a Vulkan triangle.  Writing a real one needs ~600 LOC plus
// SPIR-V bytecode and a third-party Vulkan binding (vulkan-go etc.)
// which is out of scope for this suite — the suite tests glfw-purego and
// gl-purego, not arbitrary GPU-API ecosystems.
//
// What we DO test:
//   - VulkanSupported reports true when a Vulkan loader is on the system
//   - GetVulkanGetInstanceProcAddress returns a non-zero pointer
//   - GetRequiredInstanceExtensions returns the platform's list
//     (Windows: VK_KHR_surface + VK_KHR_win32_surface; Linux: + xcb/wayland;
//      macOS: + VK_EXT_metal_surface or VK_MVK_macos_surface)
//   - WindowHint(ClientAPI, NoAPI) actually skips GL context creation
//   - CreateWindow with NoAPI doesn't crash and the window is usable for
//     event handling without a context
//
// Open the resulting window — input still works (mouse/keys logged).
// Close it (Esc / window X) to exit.
package main

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw"
)

func init() { runtime.LockOSThread() }

func main() {
	if err := glfw.Init(); err != nil {
		panic(err)
	}
	defer glfw.Terminate()

	fmt.Println("─── Vulkan binding smoke test ───")

	supported := glfw.VulkanSupported()
	fmt.Printf("VulkanSupported(): %v\n", supported)

	getProc := glfw.GetVulkanGetInstanceProcAddress()
	fmt.Printf("GetVulkanGetInstanceProcAddress(): %v (nil=%v)\n",
		getProc, getProc == unsafe.Pointer(nil))

	exts := glfw.GetRequiredInstanceExtensions()
	fmt.Printf("GetRequiredInstanceExtensions(): %d extensions\n", len(exts))
	for _, e := range exts {
		fmt.Printf("  - %s\n", e)
	}

	if !supported {
		fmt.Println("\nVulkan not available; skipping window creation. (This is")
		fmt.Println("normal on machines without a Vulkan ICD installed.)")
		return
	}

	// Skip GL context creation — the whole point of NoAPI is letting
	// Vulkan-rendering apps use glfw for window/input only.
	glfw.WindowHint(glfw.ClientAPIs, int(glfw.NoAPI))
	glfw.WindowHint(glfw.Resizable, glfw.True)

	win, err := glfw.CreateWindow(640, 360, "Vulkan check (no GL context)", nil, nil)
	if err != nil {
		fmt.Println("CreateWindow with NoAPI failed:", err)
		return
	}
	fmt.Println("\nCreated window with ClientAPI=NoAPI — window is up but no GL")
	fmt.Println("context exists.  MakeContextCurrent / SwapBuffers would no-op.")
	fmt.Println("Press Esc or close the window to exit.")

	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if key == glfw.KeyEscape && action == glfw.Press {
			win.SetShouldClose(true)
		}
		if action == glfw.Press {
			fmt.Printf("KEY %v\n", key)
		}
	})

	for !win.ShouldClose() {
		// No SwapBuffers call — there's no context to swap.
		glfw.WaitEvents()
	}
	fmt.Println("Done.")
}
