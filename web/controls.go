package web

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/buxtronix/phev2mqtt/client"
	"github.com/buxtronix/phev2mqtt/protocol"
	log "github.com/sirupsen/logrus"
)

// Lowercased valid modes/durations. Mirror what the MQTT handler accepts in
// cmd/mqtt.go.
var (
	climateModes = map[string]byte{
		"cool":       0x1,
		"heat":       0x2,
		"windscreen": 0x3,
	}
	climateDurations = map[int]byte{
		10: 0x0,
		20: 0x1,
		30: 0x2,
	}
)

// StartClimateOnClient activates climate control on the given vehicle. It
// handles the model-year-specific register layout. mode is one of
// cool|heat|windscreen, duration is 10|20|30.
func StartClimateOnClient(c *client.Client, mode string, duration int) error {
	if c == nil {
		return fmt.Errorf("not connected to vehicle")
	}
	modeCode, ok := climateModes[mode]
	if !ok {
		return fmt.Errorf("invalid climate mode %q (want cool|heat|windscreen)", mode)
	}
	durationCode, ok := climateDurations[duration]
	if !ok {
		return fmt.Errorf("invalid climate duration %d (want 10|20|30)", duration)
	}

	switch c.ModelYear {
	case client.ModelYear18, client.ModelYear24:
		state := byte(0x02)
		if err := c.SetRegister(protocol.SetACModeRegisterMY18, []byte{state, modeCode, durationCode, 0x0}); err != nil {
			return fmt.Errorf("setting AC mode (MY18+): %w", err)
		}
	case client.ModelYear14:
		// MY2014 path mirrors the /set/climate/* handling in cmd/mqtt.go.
		// First nudge register 0x05 with a simple combined byte, then
		// flip the AC-enabled register. The MQTT handler falls back to
		// register SetACModeRegisterMY14 if the timer write fails; we
		// keep that behavior for parity.
		timerPayload := make([]byte, 16)
		timerPayload[0] = 0x01
		timerPayload[1] = modeCode | durationCode

		if err := c.SetRegister(0x05, timerPayload); err != nil {
			fallback := make([]byte, 15)
			for i := range fallback {
				fallback[i] = 0xff
			}
			fallback[0] = 0x0
			fallback[1] = 0x0
			fallback[6] = modeCode | durationCode
			if err := c.SetRegister(protocol.SetACModeRegisterMY14, fallback); err != nil {
				return fmt.Errorf("setting AC mode (MY14 fallback): %w", err)
			}
		}

		if err := c.SetRegister(protocol.SetACEnabledRegisterMY14, []byte{0x02}); err != nil {
			return fmt.Errorf("enabling AC (MY14): %w", err)
		}
	default:
		return fmt.Errorf("unknown vehicle model year — try again once initial handshake completes")
	}
	return nil
}

// StopClimateOnClient terminates climate control / acknowledges termination.
// Same call as /set/climate/stop in cmd/mqtt.go.
func StopClimateOnClient(c *client.Client) error {
	if c == nil {
		return fmt.Errorf("not connected to vehicle")
	}
	switch c.ModelYear {
	case client.ModelYear14:
		if err := c.SetRegister(protocol.SetACEnabledRegisterMY14, []byte{0x01}); err != nil {
			return fmt.Errorf("disabling AC (MY14): %w", err)
		}
	}
	if err := c.SetRegister(protocol.SetAckPreACTermRegister, []byte{0x1}); err != nil {
		return fmt.Errorf("acknowledging climate termination: %w", err)
	}
	return nil
}

// SetParkingLightsOnClient toggles parking lights.
func SetParkingLightsOnClient(c *client.Client, on bool) error {
	if c == nil {
		return fmt.Errorf("not connected to vehicle")
	}
	v := byte(0x2)
	if on {
		v = 0x1
	}
	if err := c.SetRegister(0x0b, []byte{v}); err != nil {
		return fmt.Errorf("setting parking lights: %w", err)
	}
	return nil
}

// SetHeadlightsOnClient toggles headlights.
func SetHeadlightsOnClient(c *client.Client, on bool) error {
	if c == nil {
		return fmt.Errorf("not connected to vehicle")
	}
	v := byte(0x2)
	if on {
		v = 0x1
	}
	if err := c.SetRegister(0x0a, []byte{v}); err != nil {
		return fmt.Errorf("setting headlights: %w", err)
	}
	return nil
}

// CancelChargeTimerOnClient disables the charge timer. Same two register
// writes the MQTT handler does for /set/cancelchargetimer.
func CancelChargeTimerOnClient(c *client.Client) error {
	if c == nil {
		return fmt.Errorf("not connected to vehicle")
	}
	if err := c.SetRegister(0x17, []byte{0x1}); err != nil {
		return fmt.Errorf("cancel charge timer step 1: %w", err)
	}
	if err := c.SetRegister(0x17, []byte{0x11}); err != nil {
		return fmt.Errorf("cancel charge timer step 2: %w", err)
	}
	return nil
}

// SetClimateTimerOnClient writes one of the five climate timer slots
// (1..5). Mirrors the MQTT handler at cmd/mqtt.go:873 — validates input,
// encodes a RegisterClimateTimer with the target slot enabled and the
// others disabled, and writes register 0x1a.
func SetClimateTimerOnClient(c *client.Client, slot int, t ClimateTimer) error {
	if c == nil {
		return fmt.Errorf("not connected to vehicle")
	}
	if slot < 1 || slot > 5 {
		return fmt.Errorf("invalid timer slot %d (want 1..5)", slot)
	}
	if t.Enabled {
		if t.Hour < 0 || t.Hour > 23 || t.Minute < 0 || t.Minute > 50 || t.Minute%10 != 0 {
			return fmt.Errorf("invalid time %02d:%02d (hour 0-23, minute 0/10/20/30/40/50)", t.Hour, t.Minute)
		}
		if t.Duration != 10 && t.Duration != 20 && t.Duration != 30 {
			return fmt.Errorf("invalid duration %d (want 10|20|30)", t.Duration)
		}
		if _, ok := climateModes[t.Mode]; !ok {
			return fmt.Errorf("invalid mode %q (want cool|heat|windscreen)", t.Mode)
		}
	}

	reg := &protocol.RegisterClimateTimer{}
	for i := 0; i < 5; i++ {
		reg.Timers[i].Enabled = false
	}
	reg.Timers[slot-1] = protocol.ClimateTimer{
		Enabled:  t.Enabled,
		Hour:     uint8(t.Hour),
		Minute:   uint8(t.Minute),
		Mode:     t.Mode,
		Duration: uint8(t.Duration),
		Days:     t.Days,
	}

	encoded := reg.Encode()
	if err := c.SetRegister(protocol.SetClimateTimerRegister, encoded.Data); err != nil {
		return fmt.Errorf("setting climate timer %d: %w", slot, err)
	}
	return nil
}

// execReboot triggers `sudo reboot` after a short delay so callers can
// return a response first. Lifted from cmd/mqtt.go:/set/system/reboot.
func execReboot() error {
	log.Warnf("Reboot requested via web UI, rebooting in 5 seconds...")
	go func() {
		time.Sleep(5 * time.Second)
		if err := exec.Command("sudo", "reboot").Run(); err != nil {
			log.Errorf("Failed to reboot: %v", err)
		}
	}()
	return nil
}

// ClearClimateTimersOnClient disables all five timer slots. 16-byte payload
// copied from cmd/mqtt.go:937 (/set/climate/timer/clear).
func ClearClimateTimersOnClient(c *client.Client) error {
	if c == nil {
		return fmt.Errorf("not connected to vehicle")
	}
	data := make([]byte, 16)
	data[0] = 0x00
	for i := 1; i < 16; i += 3 {
		data[i] = 0xfe
		data[i+1] = 0x07
		data[i+2] = 0x00
	}
	data[15] = 0x01
	if err := c.SetRegister(protocol.SetClimateTimerRegister, data); err != nil {
		return fmt.Errorf("clearing climate timers: %w", err)
	}
	return nil
}
