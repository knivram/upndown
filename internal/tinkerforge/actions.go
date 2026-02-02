package tinkerforge

import (
	"context"
	"log"

	"github.com/Tinkerforge/go-api-bindings/distance_ir_v2_bricklet"
)

const (
	// Hight in millimeters
	PositionUp   = 1095
	PositionDown = 670
)

func (c *Client) GoTo(position uint16) uint64 {
	// Stop any ongoing movement first
	c.Stop()

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
	c.mu.Lock()

	// Create a new context for this operation
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelFunc = cancel

	log.Printf("Starting up to %d...", position)
	c.industrialDualRelay.SetValue(false, true)
	c.distanceIR.SetDistanceCallbackConfiguration(10, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0)

	var callbackID uint64
	callbackID = c.distanceIR.RegisterDistanceCallback(func(distance uint16) {
		select {
		case <-ctx.Done():
			// Operation was cancelled
			return
		default:
			if distance > position {
				c.mu.Lock()
				c.industrialDualRelay.SetValue(true, true)
				c.distanceIR.DeregisterDistanceCallback(callbackID)
				c.activeCallbackID = 0
				c.cancelFunc = nil
				c.mu.Unlock()
				log.Println("Up stopped at position:", distance)
			}
		}
	})

	c.activeCallbackID = callbackID
	c.mu.Unlock()

	log.Println("CallbackID", callbackID)
	return callbackID
}

func (c *Client) moveDownTo(position uint16) uint64 {
	c.mu.Lock()

	// Create a new context for this operation
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelFunc = cancel

	log.Printf("Starting down to %d...", position)
	c.industrialDualRelay.SetValue(true, false)
	c.distanceIR.SetDistanceCallbackConfiguration(10, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0)

	var callbackID uint64
	callbackID = c.distanceIR.RegisterDistanceCallback(func(distance uint16) {
		select {
		case <-ctx.Done():
			// Operation was cancelled
			return
		default:
			if distance < position {
				c.mu.Lock()
				c.industrialDualRelay.SetValue(true, true)
				c.distanceIR.DeregisterDistanceCallback(callbackID)
				c.activeCallbackID = 0
				c.cancelFunc = nil
				c.mu.Unlock()
				log.Println("Down stopped at position:", distance)
			}
		}
	})

	c.activeCallbackID = callbackID
	c.mu.Unlock()

	log.Println("CallbackID", callbackID)
	return callbackID
}
