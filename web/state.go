package web

import "time"

// Snapshot is the JSON-friendly view of vehicle state exposed by /api/status.
type Snapshot struct {
	VIN              string            `json:"vin"`
	Registrations    int               `json:"registrations"`
	ECUVersion       string            `json:"ecu_version"`
	Battery          int               `json:"battery_level"`
	Charging         bool              `json:"charging"`
	ChargeRemaining  int               `json:"charge_remaining_min"`
	ChargerConnected bool              `json:"charger_connected"`
	DoorsLocked      bool              `json:"doors_locked"`
	Doors            map[string]bool   `json:"doors"`
	ParkingLights    bool              `json:"parking_lights"`
	Headlights       bool              `json:"headlights"`
	InteriorLights   bool              `json:"interior_lights"`
	HazardLights     bool              `json:"hazard_lights"`
	ClimateMode      string            `json:"climate_mode"`
	ClimateState     string            `json:"climate_state"`
	ClimateTimers    [5]ClimateTimer   `json:"climate_timers"`
	GPS              *GPSData          `json:"gps,omitempty"`
	GPSEnabled       bool              `json:"gps_enabled"`
	System           *SystemInfo       `json:"system,omitempty"`
	Connections      ConnectionInfo    `json:"connections"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// ClimateTimer mirrors protocol.ClimateTimer with JSON-friendly types so
// uint8 fields aren't unmarshalled as base64 strings.
type ClimateTimer struct {
	Enabled  bool     `json:"enabled"`
	Hour     int      `json:"hour"`     // 0-23
	Minute   int      `json:"minute"`   // 0,10,20,30,40,50
	Mode     string   `json:"mode"`     // cool|heat|windscreen
	Duration int      `json:"duration"` // 10|20|30
	Days     []string `json:"days"`     // sun,mon,tue,wed,thu,fri,sat
}

// SystemInfo holds host-machine health metrics (Linux /proc + /sys).
// All values are zero on platforms where the readers fail.
type SystemInfo struct {
	CPUTempC      float64 `json:"cpu_temp_c"`
	MemoryPercent float64 `json:"memory_percent"`
	CPULoadPct    float64 `json:"cpu_load_percent"`
	DiskPercent   float64 `json:"disk_percent"`
	UptimeSec     int64   `json:"uptime_sec"`
}

// ConnectionInfo describes the state of the two external links that the
// service depends on: the MQTT broker, and the WiFi/TCP session to the PHEV
// (the car). MQTTAvailable/GPSAvailable/RebootAvailable/BridgePausable are
// false in modes where those controls don't apply so the UI can hide them.
type ConnectionInfo struct {
	MQTTAvailable    bool   `json:"mqtt_available"`
	MQTTConnected    bool   `json:"mqtt_connected"`
	MQTTBroker       string `json:"mqtt_broker"`
	PhevConnected    bool   `json:"phev_connected"`
	PhevAddress      string `json:"phev_address"`
	PhevLastSeenSec  int64  `json:"phev_last_seen_sec"` // seconds since last successful session; -1 if never
	BridgePausable   bool   `json:"bridge_pausable"`    // false in standalone mode
	BridgePaused     bool   `json:"bridge_paused"`
	GPSAvailable     bool   `json:"gps_available"`      // false in standalone mode
	RebootAvailable  bool   `json:"reboot_available"`
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
	ReconnectMQTT() error
	ReconnectPhev() error
	SetClimateTimer(slot int, t ClimateTimer) error
	ClearClimateTimers() error
	SetGPSEnabled(on bool) error
	SetBridgePaused(paused bool) error
	RebootHost() error
}
