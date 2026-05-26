package web

import "time"

// Snapshot is the JSON-friendly view of vehicle state exposed by /api/status.
type Snapshot struct {
	VIN              string          `json:"vin"`
	Battery          int             `json:"battery_level"`
	Charging         bool            `json:"charging"`
	ChargeRemaining  int             `json:"charge_remaining_min"`
	ChargerConnected bool            `json:"charger_connected"`
	DoorsLocked      bool            `json:"doors_locked"`
	Doors            map[string]bool `json:"doors"`
	ParkingLights    bool            `json:"parking_lights"`
	Headlights       bool            `json:"headlights"`
	InteriorLights   bool            `json:"interior_lights"`
	HazardLights     bool            `json:"hazard_lights"`
	ClimateMode      string          `json:"climate_mode"`
	ClimateState     string          `json:"climate_state"`
	GPS              *GPSData        `json:"gps,omitempty"`
	Connected        bool            `json:"connected"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// GPSData holds optional GPS data when the standalone driver doesn't provide
// it (no GPS attached, etc.) the field is nil and is omitted from JSON.
type GPSData struct {
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Altitude   float64 `json:"altitude"`
	Speed      float64 `json:"speed"`
	Heading    float64 `json:"heading"`
	Satellites int     `json:"satellites"`
	FixQuality int     `json:"fix_quality"`
	Timestamp  int64   `json:"timestamp"`
}

// StateProvider is what HTTP handlers depend on. It's implemented by both the
// standalone SnapshotDriver and the embedded mqttClient adapter so the
// handlers don't care where state comes from.
type StateProvider interface {
	Snapshot() Snapshot
	StartClimate(mode string, duration int) error
	StopClimate() error
	SetParkingLights(on bool) error
	SetHeadlights(on bool) error
	CancelChargeTimer() error
}
