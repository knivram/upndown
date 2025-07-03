# UpnDown

A Go application that provides hotkey-controlled automation for motorized standing desks using Tinkerforge hardware. Control your desk height with simple keyboard shortcuts while automatically stopping at precise positions using distance sensors.

## What is UpnDown?

UpnDown is a desktop automation tool that allows you to control a motorized standing desk through global hotkeys. The application integrates with Tinkerforge hardware to:

- **Control desk movement** via relay-controlled motors
- **Monitor position** using infrared distance sensors  
- **Automatically stop** at predefined heights (1060mm for up, 670mm for down)
- **Provide global hotkeys** that work system-wide on macOS

Simply press `Shift+Cmd+F11` to raise your desk or `Shift+Cmd+F12` to lower it, and the system will automatically stop when the desired position is reached.

## Technologies Used

### Core Technologies
- **[Go](https://golang.org/)** - Main programming language (Go 1.24.1)
- **[golang.design/x/hotkey](https://golang.design/x/hotkey)** - Cross-platform global hotkey registration
- **[Tinkerforge Go API](https://github.com/Tinkerforge/go-api-bindings)** - Hardware control and sensor integration

### Hardware Components
- **[Distance IR v2 Bricklet](https://www.tinkerforge.com/en/doc/Hardware/Bricklets/Distance_IR_V2.html)** - Infrared distance sensor for position detection
- **[Industrial Dual Relay Bricklet](https://www.tinkerforge.com/en/doc/Hardware/Bricklets/Industrial_Dual_Relay.html)** - Relay control for motor operations
- **[Tinkerforge Daemon](https://www.tinkerforge.com/en/doc/Software/Brickd.html)** - Hardware communication layer

## How It Works

### Architecture Overview

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────────┐
│   Global        │    │   UpnDown        │    │   Tinkerforge       │
│   Hotkeys       │───▶│   Application    │───▶│   Hardware          │
│                 │    │                  │    │                     │
│ Shift+Cmd+F11   │    │ • Hotkey Manager │    │ • Distance Sensor   │
│ Shift+Cmd+F12   │    │ • Action Control │    │ • Relay Control     │
└─────────────────┘    └──────────────────┘    └─────────────────────┘
```

### Control Flow

1. **Hotkey Registration**: On startup, the application registers global hotkeys using the `golang.design/x/hotkey` library
2. **Hardware Connection**: Establishes TCP connection to Tinkerforge daemon (localhost:4223)
3. **Action Execution**: When a hotkey is pressed:
   - Activates appropriate relay to start motor movement
   - Registers distance sensor callback for position monitoring
   - Monitors distance readings in real-time
4. **Automatic Stopping**: When target position is reached:
   - Deactivates relay to stop motor
   - Unregisters distance callback
   - Logs completion

### Position Control

The system uses predefined position thresholds:
- **Up Position**: 1060mm (desk fully raised)
- **Down Position**: 670mm (desk fully lowered)

Distance readings are continuously monitored during movement, ensuring precise positioning and preventing over-travel.

### Thread Safety

The application handles concurrent operations safely:
- **Main Thread**: Required for hotkey event handling on macOS
- **Goroutines**: Each hotkey runs in its own goroutine for non-blocking operation
- **Synchronization**: Mutex protection for hotkey registration/deregistration

## Development

### Prerequisites

- **Go 1.24.1+** - Download from [golang.org](https://golang.org/dl/)
- **Tinkerforge Brick Daemon** - Install from [Tinkerforge website](https://www.tinkerforge.com/en/doc/Software/Brickd_Install_MacOSX.html)
- **Hardware Setup**: Distance IR v2 Bricklet and Industrial Dual Relay Bricklet connected to your Tinkerforge stack

### Hardware Configuration

Before running the application, ensure your hardware is properly configured:

1. **Connect Hardware**: Attach your Tinkerforge bricklets to the stack
2. **Install Tinkerforge Brick Deamon**: Install the Tinkerforge Brick Deamon from the Tinkerforge website: https://www.tinkerforge.com/en/doc/Software/Brickd_Install_MacOSX.html
3. **Start Brick Daemon**: Run `sudo launchctl start com.tinkerforge.brickd` (macOS)
4. **Verify UIDs**: Update the UIDs in `internal/tinkerforge/client.go` to match your hardware:
   ```go
   const (
       DistanceIRV2BrickletUID        = "YZ2"  // Your distance sensor UID
       IndustrialDualRelayBrickletUID = "2bFm" // Your relay UID
   )
   ```

### Building and Running

```bash
# Clone the repository
git clone https://github.com/knivram/upndown.git
cd upndown

# Install dependencies
go mod download

# Build the application
go build -o bin/upndown cmd/upndown/main.go

# Run the application
./bin/upndown
```

### Development Workflow

#### Making Changes

1. **Edit Configuration**: Modify hotkey bindings in `internal/config/hotkeys.go`
2. **Adjust Positions**: Update target positions in `internal/tinkerforge/actions.go`
3. **Add Actions**: Implement new hardware actions in the `tinkerforge` package
4. **Test Changes**: Build and test with your hardware setup

#### Code Structure

```
├── cmd/upndown/           # Application entry point
├── internal/
│   ├── config/            # Hotkey configuration
│   ├── hotkey/            # Hotkey management
│   └── tinkerforge/       # Hardware control
```

#### Key Files to Modify

- **`internal/config/hotkeys.go`**: Add or modify hotkey bindings
- **`internal/tinkerforge/actions.go`**: Adjust position thresholds or add new movements
- **`internal/tinkerforge/client.go`**: Update hardware UIDs or connection settings

#### Testing

```bash
# Run with verbose logging
go run cmd/upndown/main.go

# Build and test binary
go build -o bin/upndown cmd/upndown/main.go
./bin/upndown
```
