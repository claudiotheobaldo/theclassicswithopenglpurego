module github.com/ClaudioTheobaldo/TheClassicsWithOpenGLPurego

go 1.25.0

require (
	github.com/ClaudioTheobaldo/gl-purego v0.0.0
	github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw v0.0.0
)

require (
	github.com/ebitengine/purego v0.8.2 // indirect
	golang.org/x/sys v0.43.0 // indirect
)

replace github.com/ClaudioTheobaldo/gl-purego => ../../Libraries/gl-purego

replace github.com/ClaudioTheobaldo/glfw-purego/v3.3/glfw => ../../Libraries/glfw-purego/v3.3/glfw
