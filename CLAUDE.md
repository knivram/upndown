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

# Run tests (if any exist)
go test ./...
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

## Hardware Integration

The application requires Tinkerforge hardware setup:
- Distance IR v2 Bricklet for position sensing (UID: "XXX" - needs configuration)
- Industrial Dual Relay Bricklet for motor control (UID: "XXX" - needs configuration)
- Tinkerforge daemon running on localhost:4223

Position thresholds are defined in `internal/tinkerforge/actions.go`:
- PositionUp: 1200mm
- PositionDown: 8000mm

## Platform Requirements

- macOS (uses mainthread.Init for hotkey handling)
- Requires accessibility permissions for global hotkey registration