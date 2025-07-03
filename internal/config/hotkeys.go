package config

import "golang.design/x/hotkey"

type HotkeyConfig struct {
	Modifiers []hotkey.Modifier
	Key       hotkey.Key
	Action    func()
	Desc      string
}

type TinkerforgeClient interface {
	Up() uint64
	Down() uint64
}

func GetHotkeyConfig(client TinkerforgeClient) []HotkeyConfig {
	return []HotkeyConfig{
		{
			Modifiers: []hotkey.Modifier{hotkey.ModShift, hotkey.ModCmd},
			Key:       hotkey.KeyF11,
			Action:    func() { client.Up() },
			Desc:      "Up",
		},
		{
			Modifiers: []hotkey.Modifier{hotkey.ModShift, hotkey.ModCmd},
			Key:       hotkey.KeyF12,
			Action:    func() { client.Down() },
			Desc:      "Down",
		},
	}
}
