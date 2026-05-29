# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go application that provides global hotkey functionality to control physical hardware via Tinkerforge bricklets. The application registers system-wide keyboard shortcuts that trigger Up/Down movements using industrial relay controls and distance IR sensors for positioning feedback.

## Build and Run Commands

```bash
# Build the application
go build -o upndown cmd/upndown/main.go

# Run the application directly
go run cmd/upndown/main.go

# Install dependencies
go mod tidy

# Run tests (the tinkerforge movement logic is covered with fakes; no hardware needed)
go test ./...
go test -race ./...
```

## Architecture

The application follows a clean architecture pattern with three main internal packages:

- **`internal/config`**: Defines hotkey configurations and bindings. Currently maps Shift+Cmd+F11/F12 to Up/Down actions
- **`internal/hotkey`**: Manages global hotkey registration and event handling using golang.design/x/hotkey library
- **`internal/tinkerforge`**: Handles communication with Tinkerforge hardware bricklets (Distance IR v2 and Industrial Dual Relay)

The main application (`cmd/upndown/main.go`) orchestrates these components:
1. Connects to Tinkerforge daemon on localhost:4223
2. Registers hotkeys from configuration
3. Runs event loop until shutdown signal

### Movement model

`tinkerforge.Client` runs a single **movement-owner goroutine** (`run()`) that is the
sole owner of all hardware I/O — so a slow or timing-out bricklet call can never block
the hotkey handlers. `GoTo(target)` is non-blocking: it hands the press to the worker via
a small buffered FIFO channel (presses beyond the buffer are dropped and logged).

A button press is a **toggle**, decided by the worker:
- **idle** → start moving toward that button's target;
- **moving** → stop the platform where it is (this applies to *either* button, same or
  opposite direction) and do **not** start a new move; pressing again starts a fresh move.

A move also stops on its own when it reaches the target. Stop detection is done **in code**:
during a move the sensor streams changed distance samples (`SetDistanceCallbackConfiguration`
with `ThresholdOptionOff`, `valueHasToChange=true`) and the callback compares the reading to
the target. (The firmware `Smaller`/`Greater` thresholds were tried first but the `<`
threshold did not fire reliably for downward moves, letting the platform run past the
target.) Streaming is turned fully off when idle. A `positionTolerance` deadband makes a
press toward a target you are already at a no-op. A move that never reaches its target is
force-stopped after `maxMoveDuration` (runaway guard); the relay is set to a known-safe
state on connect and stopped on shutdown.

Movement state (`moving`, `activeCallbackID`, `moveEpoch`) is owned exclusively by the
worker goroutine and needs no locking; the sensor callback only does a non-blocking send
of a `reachEvent` tagged with the move's epoch, so signals from a finished or superseded
move are ignored.

## Hardware Integration

The application requires Tinkerforge hardware setup (UIDs are set in `internal/tinkerforge/client.go`):
- Distance IR v2 Bricklet for position sensing (UID: "YZ2")
- Industrial Dual Relay Bricklet for motor control (UID: "2bFm")
- Tinkerforge daemon running on localhost:4223

Position thresholds are defined in `internal/tinkerforge/actions.go` (distance in mm as
reported by the sensor; a *larger* reading means the platform is *higher*):
- PositionUp: 1095mm
- PositionDown: 670mm

Relay wiring (`SetValue(channel0, channel1)`): up = `(false, true)`, down = `(true, false)`,
stop = `(true, true)`. These values must match the physical wiring.

## Logging

Uses the stdlib `log/slog` text handler writing to stderr. Under launchd, stdout/stderr are
redirected to `~/Library/Logs/upndown.log`. The level is controlled by the
`UPNDOWN_LOG_LEVEL` env var (`debug|info|warn|error`, default `info`); `debug` adds
per-target queueing and listener registration lines.

## Platform Requirements

- macOS (uses mainthread.Init for hotkey handling)
- Requires accessibility permissions for global hotkey registration