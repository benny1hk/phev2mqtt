package web

import (
	"context"
	"sync"
	"time"

	"github.com/buxtronix/phev2mqtt/client"
	"github.com/buxtronix/phev2mqtt/protocol"
	log "github.com/sirupsen/logrus"
)

// SnapshotDriver maintains a long-lived connection to a PHEV and exposes the
// most recent state via Snapshot(). It is the StateProvider used by the
// standalone `phev2mqtt web` subcommand.
type SnapshotDriver struct {
	address string

	mu        sync.RWMutex
	snap      Snapshot
	cl        *client.Client // current client; nil if disconnected
	connected bool
}

// NewSnapshotDriver returns a driver that will connect to the given address
// when Run is called.
func NewSnapshotDriver(address string) *SnapshotDriver {
	d := &SnapshotDriver{
		address: address,
	}
	d.snap.Doors = map[string]bool{}
	return d
}

// Run blocks driving the reconnect/read loop until ctx is cancelled. Failures
// to connect are logged and retried with a short backoff.
func (d *SnapshotDriver) Run(ctx context.Context) {
	backoff := 5 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := d.session(ctx); err != nil {
			log.Errorf("PHEV session ended: %v", err)
		}
		d.markDisconnected()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (d *SnapshotDriver) session(ctx context.Context) error {
	cl, err := client.New(client.AddressOption(d.address))
	if err != nil {
		return err
	}
	if err := cl.Connect(); err != nil {
		return err
	}
	if err := cl.Start(); err != nil {
		cl.Close()
		return err
	}

	d.mu.Lock()
	d.cl = cl
	d.connected = true
	d.snap.Connected = true
	d.mu.Unlock()

	defer func() {
		cl.Close()
		d.mu.Lock()
		d.cl = nil
		d.connected = false
		d.snap.Connected = false
		d.mu.Unlock()
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Kick a register read immediately so the snapshot fills out quickly.
	_ = cl.SetRegister(0x6, []byte{0x3})

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = cl.SetRegister(0x6, []byte{0x3})
		case msg, ok := <-cl.Recv:
			if !ok {
				return nil
			}
			if msg.Reg == nil {
				continue
			}
			d.applyRegister(msg.Reg)
		}
	}
}

func (d *SnapshotDriver) markDisconnected() {
	d.mu.Lock()
	d.connected = false
	d.snap.Connected = false
	d.mu.Unlock()
}

func (d *SnapshotDriver) applyRegister(reg protocol.Register) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.snap.Doors == nil {
		d.snap.Doors = map[string]bool{}
	}
	switch r := reg.(type) {
	case *protocol.RegisterVIN:
		d.snap.VIN = r.VIN
	case *protocol.RegisterBatteryLevel:
		if r.Level > 5 && r.Level < 255 {
			d.snap.Battery = r.Level
		}
		d.snap.ParkingLights = r.ParkingLights
	case *protocol.RegisterChargeStatus:
		d.snap.Charging = r.Charging
		if r.Remaining < 1000 {
			d.snap.ChargeRemaining = r.Remaining
		}
	case *protocol.RegisterChargePlug:
		d.snap.ChargerConnected = r.Connected
	case *protocol.RegisterDoorStatus:
		d.snap.DoorsLocked = r.Locked
		d.snap.Headlights = r.Headlights
		d.snap.Doors["bonnet"] = r.Bonnet
		d.snap.Doors["boot"] = r.Boot
		d.snap.Doors["driver"] = r.Driver
		d.snap.Doors["front_passenger"] = r.FrontPassenger
		d.snap.Doors["rear_left"] = r.RearLeft
		d.snap.Doors["rear_right"] = r.RearRight
	case *protocol.RegisterLightStatus:
		d.snap.InteriorLights = r.Interior
		d.snap.HazardLights = r.Hazard
	case *protocol.RegisterACMode:
		d.snap.ClimateMode = r.Mode
	case *protocol.RegisterPreACState:
		switch r.State {
		case protocol.PreACOff:
			d.snap.ClimateState = "off"
		case protocol.PreACOn:
			d.snap.ClimateState = "on"
		case protocol.PreACTerminated:
			d.snap.ClimateState = "terminated"
		default:
			d.snap.ClimateState = "unknown"
		}
	}
	d.snap.UpdatedAt = time.Now()
}

// Snapshot returns the most recent state. The returned Snapshot is a copy of
// the internal data so callers can hold onto it without locks.
func (d *SnapshotDriver) Snapshot() Snapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()
	s := d.snap
	if d.snap.Doors != nil {
		s.Doors = make(map[string]bool, len(d.snap.Doors))
		for k, v := range d.snap.Doors {
			s.Doors[k] = v
		}
	}
	return s
}

func (d *SnapshotDriver) activeClient() *client.Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cl
}

// StartClimate, StopClimate, etc satisfy StateProvider.

func (d *SnapshotDriver) StartClimate(mode string, duration int) error {
	return StartClimateOnClient(d.activeClient(), mode, duration)
}

func (d *SnapshotDriver) StopClimate() error {
	return StopClimateOnClient(d.activeClient())
}

func (d *SnapshotDriver) SetParkingLights(on bool) error {
	return SetParkingLightsOnClient(d.activeClient(), on)
}

func (d *SnapshotDriver) SetHeadlights(on bool) error {
	return SetHeadlightsOnClient(d.activeClient(), on)
}

func (d *SnapshotDriver) CancelChargeTimer() error {
	return CancelChargeTimerOnClient(d.activeClient())
}
