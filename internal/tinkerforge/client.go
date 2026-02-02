package tinkerforge

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/Tinkerforge/go-api-bindings/distance_ir_v2_bricklet"
	"github.com/Tinkerforge/go-api-bindings/industrial_dual_relay_bricklet"
	"github.com/Tinkerforge/go-api-bindings/ipconnection"
)

const (
	Host                           = "localhost"
	Port                           = 4223
	DistanceIRV2BrickletUID        = "YZ2"
	IndustrialDualRelayBrickletUID = "2bFm"
)

type Client struct {
	ipcon               *ipconnection.IPConnection
	distanceIR          *distance_ir_v2_bricklet.DistanceIRV2Bricklet
	industrialDualRelay *industrial_dual_relay_bricklet.IndustrialDualRelayBricklet
	mu                  sync.Mutex
	cancelFunc          context.CancelFunc
	activeCallbackID    uint64
}

func NewClient() *Client {
	ipcon := ipconnection.New()
	distanceIR, err := distance_ir_v2_bricklet.New(DistanceIRV2BrickletUID, &ipcon)
	if err != nil {
		log.Fatalf("Failed to create distance IR v2 bricklet: %v", err)
	}
	industrialDualRelay, err := industrial_dual_relay_bricklet.New(IndustrialDualRelayBrickletUID, &ipcon)
	if err != nil {
		log.Fatalf("Failed to create industrial dual relay bricklet: %v", err)
	}
	return &Client{
		ipcon:               &ipcon,
		distanceIR:          &distanceIR,
		industrialDualRelay: &industrialDualRelay,
	}
}

func (c *Client) Connect() error {
	return c.ipcon.Connect(fmt.Sprintf("%s:%d", Host, Port))
}

func (c *Client) Disconnect() {
	if c.ipcon != nil {
		c.ipcon.Disconnect()
	}
}

// Stop cancels any ongoing movement and stops the relays
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Cancel any ongoing operation
	if c.cancelFunc != nil {
		c.cancelFunc()
		c.cancelFunc = nil
	}

	// Deregister active callback if any
	if c.activeCallbackID != 0 {
		c.distanceIR.DeregisterDistanceCallback(c.activeCallbackID)
		c.activeCallbackID = 0
	}

	// Stop both relays (true, true means both are off/stopped)
	c.industrialDualRelay.SetValue(true, true)
	log.Println("Movement stopped")
}
