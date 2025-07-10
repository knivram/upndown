package config

import (
	"github.com/knivram/upndown/internal/tinkerforge"
	"golang.design/x/hotkey"
)

type HotkeyConfig struct {
	Modifiers []hotkey.Modifier
	Key       hotkey.Key
	Action    func()
	Desc      string
}

type TinkerforgeClient interface {
	GoTo(position uint16) uint64
}

func GetHotkeyConfig(client TinkerforgeClient) []HotkeyConfig {
	return []HotkeyConfig{
		{
			Modifiers: []hotkey.Modifier{hotkey.ModShift, hotkey.ModCmd},
			Key:       hotkey.KeyF11,
			Action:    func() { client.GoTo(tinkerforge.PositionUp) },
			Desc:      "Up",
		},
		{
			Modifiers: []hotkey.Modifier{hotkey.ModShift, hotkey.ModCmd},
			Key:       hotkey.KeyF12,
			Action:    func() { client.GoTo(tinkerforge.PositionDown) },
			Desc:      "Down",
		},
	}
}
