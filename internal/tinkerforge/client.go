package tinkerforge

import (
	"fmt"
	"log"

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
