package tinkerforge

import (
	"log"

	"github.com/Tinkerforge/go-api-bindings/distance_ir_v2_bricklet"
)

const (
	// Hight in millimeters
	PositionUp   = 1095
	PositionDown = 670
)

func (c *Client) GoTo(position uint16) uint64 {
	currentPosition, err := c.distanceIR.GetDistance()
	if err != nil {
		log.Println("Error getting current position:", err)
		return 0
	}
	log.Printf("Current position: %d, target position: %d", currentPosition, position)
	if currentPosition < position {
		return c.moveUpTo(position)
	}
	if currentPosition > position {
		return c.moveDownTo(position)
	}
	log.Println("Current position is the same as the target position")
	return 0
}

func (c *Client) moveUpTo(position uint16) uint64 {
	log.Printf("Starting up to %d...", position)
	c.industrialDualRelay.SetValue(false, true)
	c.distanceIR.SetDistanceCallbackConfiguration(10, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0)
	var callbackID uint64
	callbackID = c.distanceIR.RegisterDistanceCallback(func(distance uint16) {
		if distance > position {
			c.industrialDualRelay.SetValue(true, true)
			log.Println("CallbackID", callbackID)
			c.distanceIR.DeregisterDistanceCallback(callbackID)
			log.Println("Up stopped")
		}
	})
	log.Println("CallbackID", callbackID)
	return callbackID
}

func (c *Client) moveDownTo(position uint16) uint64 {
	log.Printf("Starting down to %d...", position)
	c.industrialDualRelay.SetValue(true, false)
	c.distanceIR.SetDistanceCallbackConfiguration(10, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0)
	var callbackID uint64
	callbackID = c.distanceIR.RegisterDistanceCallback(func(distance uint16) {
		if distance < position {
			c.industrialDualRelay.SetValue(true, true)
			log.Println("CallbackID", callbackID)
			c.distanceIR.DeregisterDistanceCallback(callbackID)
			log.Println("Down stopped")
		}
	})
	log.Println("CallbackID", callbackID)
	return callbackID
}
