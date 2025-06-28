package main

import (
	"log"
	"os"
	"os/signal"
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
	err := a.tinkerforgeClient.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to Tinkerforge daemon: %v", err)
	}
	defer a.tinkerforgeClient.Disconnect()

	hotkeys := config.GetHotkeyConfig(a.tinkerforgeClient)

	for _, hk := range hotkeys {
		err := a.hotkeyManager.RegisterHotkey(hk.Modifiers, hk.Key, hk.Action)
		if err != nil {
			log.Printf("Failed to register hotkey for %s: %v", hk.Desc, err)
		} else {
			log.Printf("✓ %s: %v", hk.Desc, xhotkey.New(hk.Modifiers, hk.Key))
		}
	}

	log.Println("Application started. Press hotkeys to trigger actions.")
	log.Println("Available hotkeys:")
	for _, hk := range hotkeys {
		log.Printf("  %v - %s", xhotkey.New(hk.Modifiers, hk.Key), hk.Desc)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	a.Shutdown()
}

func (a *App) Shutdown() {
	log.Println("Shutting down application...")
	a.hotkeyManager.Shutdown()
	a.tinkerforgeClient.Disconnect()
	log.Println("Application shutdown complete")
}

func main() {
	// On macOS, hotkey events must be handled on the main thread
	mainthread.Init(func() {
		app := NewApp()
		app.Run()
	})
}
