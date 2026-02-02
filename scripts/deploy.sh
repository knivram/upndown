#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
INSTALL_DIR="$HOME/.local/bin"
BINARY="upndown"

mkdir -p "$INSTALL_DIR"
go build -o "$PROJECT_DIR/$BINARY" "$PROJECT_DIR/cmd/upndown/main.go"
cp "$PROJECT_DIR/$BINARY" "$INSTALL_DIR/$BINARY"
launchctl kickstart -k "gui/$(id -u)/com.knivram.upndown"

echo "Deployed and restarted $BINARY"
