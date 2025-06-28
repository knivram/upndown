package hotkey

import (
	"context"
	"sync"

	"golang.design/x/hotkey"
)

type Manager struct {
	hotkeys []*hotkey.Hotkey
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		hotkeys: make([]*hotkey.Hotkey, 0),
		ctx:     ctx,
		cancel:  cancel,
	}
}