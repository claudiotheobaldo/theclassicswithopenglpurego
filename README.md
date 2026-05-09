# TheClassicsWithOpenGLPurego

Small classic games and demos written in pure Go on top of
[`glfw-purego`](https://github.com/ClaudioTheobaldo/glfw-purego) and
[`gl-purego`](https://github.com/ClaudioTheobaldo/gl-purego).

The point isn't the games — it's exercising the bindings against real
input/render workloads to surface flaws that in-repo smoke tests miss.

## Planned

- Pong
- Snake
- Tetris
- Breakout
- Asteroids
- Conway's Game of Life
- Mandelbrot zoomer
- Input event tape (diagnostic)

## Build

Each program lives under `cmd/<name>/` and builds with plain `go build`.
No CGO, no asset bundles.
