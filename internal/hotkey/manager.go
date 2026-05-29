package hotkey

import (
	"fmt"
	"log/slog"

	"golang.design/x/hotkey"
)

func (m *Manager) RegisterHotkey(modifiers []hotkey.Modifier, key hotkey.Key, action func()) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	hk := hotkey.New(modifiers, key)
	if err := hk.Register(); err != nil {
		return fmt.Errorf("failed to register hotkey %v: %w", hk, err)
	}

	m.hotkeys = append(m.hotkeys, hk)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-hk.Keydown():
				slog.Info("hotkey triggered", "keys", hk.String())
				action()
			case <-m.ctx.Done():
				return
			}
		}
	}()

	slog.Debug("hotkey listener registered", "keys", hk.String())
	return nil
}

func (m *Manager) UnregisterAllHotkeys() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, hk := range m.hotkeys {
		hk.Unregister()
		slog.Debug("hotkey unregistered", "keys", hk.String())
	}
	m.hotkeys = m.hotkeys[:0]
}

func (m *Manager) Shutdown() {
	m.cancel()
	m.wg.Wait()
	m.UnregisterAllHotkeys()
}
