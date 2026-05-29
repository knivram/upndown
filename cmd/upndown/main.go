package main

import (
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/knivram/upndown/internal/config"
	"github.com/knivram/upndown/internal/hotkey"
	"github.com/knivram/upndown/internal/tinkerforge"
	xhotkey "golang.design/x/hotkey"
	"golang.design/x/hotkey/mainthread"
)

type App struct {
	hotkeyManager     *hotkey.Manager
	tinkerforgeClient *tinkerforge.Client
}

func NewApp() *App {
	return &App{
		hotkeyManager:     hotkey.NewManager(),
		tinkerforgeClient: tinkerforge.NewClient(),
	}
}

func (a *App) Run() {
	if err := a.tinkerforgeClient.Connect(); err != nil {
		slog.Error("failed to connect to Tinkerforge daemon", "err", err)
		os.Exit(1)
	}
	defer a.tinkerforgeClient.Disconnect()

	hotkeys := config.GetHotkeyConfig(a.tinkerforgeClient)
	for _, hk := range hotkeys {
		if err := a.hotkeyManager.RegisterHotkey(hk.Modifiers, hk.Key, hk.Action); err != nil {
			slog.Error("failed to register hotkey", "desc", hk.Desc, "err", err)
		} else {
			slog.Info("hotkey registered", "desc", hk.Desc, "keys", xhotkey.New(hk.Modifiers, hk.Key).String())
		}
	}

	slog.Info("application started; waiting for hotkeys")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	a.Shutdown()
}

func (a *App) Shutdown() {
	slog.Info("shutting down")
	a.hotkeyManager.Shutdown()
	a.tinkerforgeClient.Disconnect()
	slog.Info("shutdown complete")
}

// setupLogging installs a structured slog logger writing to stderr (which
// launchd redirects to ~/Library/Logs/upndown.log). The level is controlled by
// UPNDOWN_LOG_LEVEL (debug|info|warn|error); default is info. Set it to debug
// to see per-target queueing and listener registration.
func setupLogging() {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("UPNDOWN_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func main() {
	setupLogging()

	// On macOS, hotkey events must be handled on the main thread.
	mainthread.Init(func() {
		app := NewApp()
		app.Run()
	})
}
