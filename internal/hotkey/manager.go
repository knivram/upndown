package hotkey

import (
	"fmt"
	"log"

	"golang.design/x/hotkey"
)

func (m *Manager) RegisterHotkey(modifiers []hotkey.Modifier, key hotkey.Key, action func()) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	hk := hotkey.New(modifiers, key)
	err := hk.Register()
	if err != nil {
		return fmt.Errorf("failed to register hotkey %v: %w", hk, err)
	}

	m.hotkeys = append(m.hotkeys, hk)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-hk.Keydown():
				log.Printf("Hotkey triggered: %v", hk)
				action()
			case <-m.ctx.Done():
				return
			}
		}
	}()

	log.Printf("Registered hotkey: %v", hk)
	return nil
}

func (m *Manager) UnregisterAllHotkeys() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, hk := range m.hotkeys {
		hk.Unregister()
		log.Printf("Unregistered hotkey: %v", hk)
	}
	m.hotkeys = m.hotkeys[:0]
}

func (m *Manager) Shutdown() {
	m.cancel()
	m.wg.Wait()
	m.UnregisterAllHotkeys()
}