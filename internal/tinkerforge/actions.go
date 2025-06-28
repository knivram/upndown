package tinkerforge

import (
	"github.com/Tinkerforge/go-api-bindings/distance_ir_v2_bricklet"
)

const (
	// Hight in millimeters
	PositionUp   = 1200
	PositionDown = 8000
)

func (c *Client) Up() {
	c.industrialDualRelay.SetValue(true, false)
	c.distanceIR.SetDistanceCallbackConfiguration(10, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0)
	c.distanceIR.RegisterDistanceCallback(func(distance uint16) {
		if distance > PositionUp {
			c.industrialDualRelay.SetValue(false, false)
		}
	})
}

func (c *Client) Down() {
	c.industrialDualRelay.SetValue(false, true)
	c.distanceIR.SetDistanceCallbackConfiguration(10, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0)
	c.distanceIR.RegisterDistanceCallback(func(distance uint16) {
		if distance < PositionDown {
			c.industrialDualRelay.SetValue(false, false)
		}
	})
}
