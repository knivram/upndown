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

A move also stops on its own when it reaches the target. Stop detection is done by
**actively polling** the sensor (a *pull* model): while a move is in progress the worker
reads `GetDistance` every `pollInterval` (50ms) and compares it to the target in code
(`checkProgress` → `reachedTarget`; up stops at `d >= target`, down at `d <= target`).
Polling replaced an earlier *push* model (`SetDistanceCallbackConfiguration` streaming +
a registered callback): when the daemon stopped pushing samples the platform ran blind to
the physical limit. Pulling does not depend on the daemon volunteering data and yields an
explicit error when the daemon is unresponsive. (The firmware `Smaller`/`Greater`
thresholds were also tried and abandoned — the `<` threshold did not fire reliably for
downward moves.)

Two watchdogs protect against a move that never reaches its target:
- **sensor-stale** (`sensorStaleTimeout`, 1.5s = 3× `requestTimeout`): if no *good* reading
  arrives for this long, the move is force-stopped — so a dead/unresponsive sensor never
  runs the platform blind. A couple of isolated `GetDistance` timeouts are tolerated
  without aborting.
- **max-duration** (`maxMoveDuration`, 15s): a hard backstop. A normal full-travel move
  takes ~10s, so 15s is genuinely tighter than a real move (the old 30s was looser than a
  normal move and so was no backstop at all).

A `positionTolerance` deadband makes a press toward a target you are already at a no-op.
The relay is set to a known-safe state on connect and stopped on shutdown.

Because all hardware I/O is synchronous and runs on the worker, the worker can be blocked
inside a single `GetDistance` poll for up to `requestTimeout` (500ms); a button press
therefore takes at most ~one `requestTimeout` to be acted on. Relay `SetValue` is
**fire-and-forget** in the bindings (response-not-expected), so `relayStop()` never blocks
and a `nil` return means "stop command queued", not "motor confirmed off" — the safety
guarantee comes from the worker deciding to stop promptly, not from confirming the relay.

Movement state (`moving`, `moveDir`, `moveTarget`, `moveStart`) is owned exclusively by the
worker goroutine and needs no locking. There are no asynchronous hardware callbacks, so no
move state needs epoch-tagging. The poll ticker is created when a move starts and torn down
on every move-ending path. The move-watchdog clock is injected (`Client.now`, default
`time.Now`) so the staleness / max-duration logic is unit-testable without real time.

## Hardware Integration

The application requires Tinkerforge hardware setup (UIDs are set in `internal/tinkerforge/client.go`):
- Distance IR v2 Bricklet for position sensing (UID: "YZ2")
- Industrial Dual Relay Bricklet for motor control (UID: "2bFm")
- Tinkerforge daemon running on localhost:4223

Position thresholds are defined in `internal/tinkerforge/actions.go` (distance in mm as
reported by the sensor; a *larger* reading means the platform is *higher*):
- PositionUp: 1095mm
- PositionDown: 670mm

Relay wiring (`SetValue(channel0, channel1)`): channel0 = Up line, channel1 = Down line,
each over COM↔NO. up = `(true, false)`, down = `(false, true)`, stop = `(false, false)`.
These values must match the physical wiring.

Stop is the de-energized state, so it is fail-safe: when the bricklet loses power (e.g. the
Mac sleeping and dropping USB power) both channels drop to `(false, false)` = stop. An
earlier wiring had stop = `(true, true)` (desk lines on the NC terminals), so a power cycle
asserted *both* buttons at once — an invalid input that locked the desk controller out and
made the first move after an idle period a silent no-op (the relay clicked but the desk did
not move). Do not invert these values without rewiring the desk lines back onto NC.

## Logging

Uses the stdlib `log/slog` text handler writing to stderr. Under launchd, stdout/stderr are
redirected to `~/Library/Logs/upndown.log`. The level is controlled by the
`UPNDOWN_LOG_LEVEL` env var (`debug|info|warn|error`, default `info`); `debug` adds
button-press queueing lines and the first failed distance read of a stale streak.

A move ends with one of: `move complete` (reached target), `move stopped` (button press or
shutdown, with `reason`), or `move aborted` (a watchdog fired, with `reason=sensor_stale`
or `reason=max_duration`). The two abort reasons distinguish the two production faults — a
flaky/dead sensor vs. a move that simply never reaches the target.

## Platform Requirements

- macOS (uses mainthread.Init for hotkey handling)
- Requires accessibility permissions for global hotkey registration