package tinkerforge

import (
	"log"

	"github.com/Tinkerforge/go-api-bindings/distance_ir_v2_bricklet"
)

const (
	// Hight in millimeters
	PositionUp   = 1060
	PositionDown = 670
)

func (c *Client) Up() uint64 {
	log.Println("Starting up...")
	c.industrialDualRelay.SetValue(false, true)
	c.distanceIR.SetDistanceCallbackConfiguration(10, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0)
	var callbackID uint64
	callbackID = c.distanceIR.RegisterDistanceCallback(func(distance uint16) {
		if distance > PositionUp {
			c.industrialDualRelay.SetValue(true, true)
			log.Println("CallbackID", callbackID)
			c.distanceIR.DeregisterDistanceCallback(callbackID)
			log.Println("Up stopped")
		}
	})
	log.Println("CallbackID", callbackID)
	return callbackID
}

func (c *Client) Down() uint64 {
	log.Println("Starting down...")
	c.industrialDualRelay.SetValue(true, false)
	c.distanceIR.SetDistanceCallbackConfiguration(10, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0)
	var callbackID uint64
	callbackID = c.distanceIR.RegisterDistanceCallback(func(distance uint16) {
		if distance < PositionDown {
			c.industrialDualRelay.SetValue(true, true)
			log.Println("CallbackID", callbackID)
			c.distanceIR.DeregisterDistanceCallback(callbackID)
			log.Println("Down stopped")
		}
	})
	log.Println("CallbackID", callbackID)
	return callbackID
}
