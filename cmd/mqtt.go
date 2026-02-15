/*
Copyright © 2021 Ben Buxton <bbuxton@gmail.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/
package cmd

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/adrianmo/go-nmea"
	"github.com/buxtronix/phev2mqtt/client"
	"github.com/buxtronix/phev2mqtt/protocol"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tarm/serial"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	log "github.com/sirupsen/logrus"
)

const defaultWifiRestartCmd = "sudo ip link set wlan0 down && sleep 3 && sudo ip link set wlan0 up"

// mqttCmd represents the mqtt command
var mqttCmd = &cobra.Command{
	Use:   "mqtt",
	Short: "Start an MQTT bridge.",
	Long: `Maintains a connected to the Phev (retry as needed) and also to an MQTT server.

Status data from the car is passed to the MQTT topics, and also some commands from MQTT
are sent to control certain aspects of the car. See the phev2mqtt Github page for
more details on the topics.
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mc := &mqttClient{climate: new(climate)}
		return mc.Run(cmd, args)
	},
}

// Tracks complete climate state as on and mode are separately
// sent by the car.
type climate struct {
	state *protocol.PreACState
	mode  *string
}

func (c *climate) setMode(m string) {
	c.mode = &m
}
func (c *climate) setState(state protocol.PreACState) {
	c.state = &state
}

func (c *climate) mqttStates() map[string]string {
	m := map[string]string{
		"/climate/state":      "off",
		"/climate/cool":       "off",
		"/climate/heat":       "off",
		"/climate/windscreen": "off",
	}
	if c.mode == nil || c.state == nil {
		return m
	}
	switch *c.state {
	case protocol.PreACOn:
		m["/climate/state"] = *c.mode
	case protocol.PreACOff:
		{
			m["/climate/state"] = "off"
			return m
		}
	case protocol.PreACTerminated:
		{
			m["/climate/state"] = "terminated"
			return m
		}
	default:
		{
			m["/climate/state"] = "unknown"
			return m
		}
	}
	m["/climate/state"] = *c.mode
	switch *c.mode {
	case "cool":
		m["/climate/cool"] = "on"
	case "heat":
		m["/climate/heat"] = "on"
	case "windscreen":
		m["/climate/windscreen"] = "on"
	}
	return m
}

// System metrics for Raspberry Pi monitoring
type systemMetrics struct {
	CPUTemp       float64 // CPU temperature in Celsius
	MemoryPercent float64 // Memory usage percentage
	CPULoad       float64 // CPU load average (1 minute)
	DiskPercent   float64 // Disk usage percentage
	UptimeSeconds int64   // System uptime in seconds
}

// getCPUTemperature reads the SoC temperature from thermal_zone0
func getCPUTemperature() (float64, error) {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0, err
	}
	tempStr := strings.TrimSpace(string(data))
	tempMilliC, err := strconv.ParseInt(tempStr, 10, 64)
	if err != nil {
		return 0, err
	}
	// Convert from millidegrees to degrees Celsius
	return float64(tempMilliC) / 1000.0, nil
}

// getMemoryUsage reads memory statistics from /proc/meminfo
func getMemoryUsage() (float64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}

	var memTotal, memAvailable int64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			memTotal, _ = strconv.ParseInt(fields[1], 10, 64)
		case "MemAvailable:":
			memAvailable, _ = strconv.ParseInt(fields[1], 10, 64)
		}
	}

	if memTotal == 0 {
		return 0, fmt.Errorf("could not read total memory")
	}

	memUsed := memTotal - memAvailable
	return float64(memUsed) / float64(memTotal) * 100.0, nil
}

// getCPULoad reads the 1-minute load average
func getCPULoad() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("could not parse loadavg")
	}

	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}

	// Normalize by number of CPUs to get percentage
	numCPU := float64(runtime.NumCPU())
	return (load / numCPU) * 100.0, nil
}

// getDiskUsage gets the disk usage percentage for root filesystem
func getDiskUsage() (float64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs("/", &stat)
	if err != nil {
		return 0, err
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free

	return float64(used) / float64(total) * 100.0, nil
}

// getUptime reads system uptime from /proc/uptime
func getUptime() (int64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("could not parse uptime")
	}

	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}

	return int64(uptime), nil
}

// collectSystemMetrics gathers all system metrics
func collectSystemMetrics() *systemMetrics {
	metrics := &systemMetrics{}

	if temp, err := getCPUTemperature(); err == nil {
		metrics.CPUTemp = temp
	} else {
		log.Debugf("Could not read CPU temperature: %v", err)
	}

	if mem, err := getMemoryUsage(); err == nil {
		metrics.MemoryPercent = mem
	} else {
		log.Debugf("Could not read memory usage: %v", err)
	}

	if load, err := getCPULoad(); err == nil {
		metrics.CPULoad = load
	} else {
		log.Debugf("Could not read CPU load: %v", err)
	}

	if disk, err := getDiskUsage(); err == nil {
		metrics.DiskPercent = disk
	} else {
		log.Debugf("Could not read disk usage: %v", err)
	}

	if uptime, err := getUptime(); err == nil {
		metrics.UptimeSeconds = uptime
	} else {
		log.Debugf("Could not read uptime: %v", err)
	}

	return metrics
}

// GPS location data
type gpsLocation struct {
	mu                sync.RWMutex
	latitude          float64
	longitude         float64
	altitude          float64
	speed             float64 // km/h
	heading           float64 // degrees
	satellites        int
	fixQuality        int
	lastUpdate        time.Time
	lastPublish       time.Time
	enabled           bool
	moving            bool
	lastMovementCheck time.Time
}

func (g *gpsLocation) update(lat, lon, alt, speed, heading float64, sats, quality int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.latitude = lat
	g.longitude = lon
	g.altitude = alt
	g.speed = speed
	g.heading = heading
	g.satellites = sats
	g.fixQuality = quality
	g.lastUpdate = time.Now()

	// Determine if moving (speed > 10 km/h)
	g.moving = speed > 10.0
}

func (g *gpsLocation) shouldPublish() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.enabled || g.fixQuality == 0 {
		return false
	}

	now := time.Now()

	// If moving, publish every 3-5 seconds
	if g.moving {
		return now.Sub(g.lastPublish) >= 3*time.Second
	}

	// If stationary, publish every 5 minutes
	return now.Sub(g.lastPublish) >= 5*time.Minute
}

func (g *gpsLocation) hasMoved(newLat, newLon float64) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Calculate distance in meters using Haversine formula
	distance := haversineDistance(g.latitude, g.longitude, newLat, newLon)

	// If moved more than 100 meters, trigger update
	return distance > 100.0
}

func (g *gpsLocation) getData() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return map[string]interface{}{
		"latitude":    g.latitude,
		"longitude":   g.longitude,
		"altitude":    g.altitude,
		"speed":       g.speed,
		"heading":     g.heading,
		"satellites":  g.satellites,
		"fix_quality": g.fixQuality,
		"timestamp":   g.lastUpdate.Unix(),
	}
}

func (g *gpsLocation) markPublished() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastPublish = time.Now()
}

func (g *gpsLocation) setEnabled(enabled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.enabled = enabled
}

func (g *gpsLocation) isEnabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.enabled
}

// haversineDistance calculates the distance between two GPS coordinates in meters
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371000.0 // Earth radius in meters

	// Convert to radians
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	deltaLat := (lat2 - lat1) * math.Pi / 180
	deltaLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadius * c
}

// openGPSSerial attempts to open GPS serial port
func openGPSSerial() (io.ReadWriteCloser, error) {
	ports := []string{"/dev/ttyUSB0", "/dev/ttyACM0"}

	for _, port := range ports {
		cfg := &serial.Config{
			Name: port,
			Baud: 9600,
		}

		s, err := serial.OpenPort(cfg)
		if err == nil {
			log.Infof("GPS connected on %s", port)
			return s, nil
		}
	}

	return nil, fmt.Errorf("could not open GPS on any port")
}

// readGPS reads NMEA sentences from GPS and updates location
func (m *mqttClient) readGPS(gps *gpsLocation) {
	for {
		if !gps.isEnabled() {
			time.Sleep(5 * time.Second)
			continue
		}

		port, err := openGPSSerial()
		if err != nil {
			log.Errorf("Failed to open GPS: %v", err)
			time.Sleep(30 * time.Second)
			continue
		}

		log.Infof("GPS reader started")
		scanner := bufio.NewScanner(port)

		for scanner.Scan() {
			if !gps.isEnabled() {
				port.Close()
				break
			}

			line := scanner.Text()

			// Parse NMEA sentence
			s, err := nmea.Parse(line)
			if err != nil {
				continue
			}

			switch msg := s.(type) {
			case nmea.RMC:
				// RMC contains position, speed, and course
				if msg.Validity == "A" { // Valid fix
					lat := msg.Latitude
					lon := msg.Longitude
					speed := msg.Speed * 1.852 // Convert knots to km/h
					heading := msg.Course

					// Check if moved significantly
					if gps.hasMoved(lat, lon) || gps.shouldPublish() {
						// Get altitude and satellites from last GGA
						data := gps.getData()
						alt := data["altitude"].(float64)
						sats := data["satellites"].(int)
						quality := data["fix_quality"].(int)

						gps.update(lat, lon, alt, speed, heading, sats, quality)

						if gps.shouldPublish() {
							m.publishGPS(gps)
							gps.markPublished()
						}
					}
				}

			case nmea.GGA:
				// GGA contains altitude and satellite info
				if msg.FixQuality != "0" {
					gps.mu.Lock()
					gps.altitude = msg.Altitude
					gps.satellites = int(msg.NumSatellites)
					qual, _ := strconv.Atoi(msg.FixQuality)
					gps.fixQuality = qual
					gps.mu.Unlock()
				}
			}
		}

		if err := scanner.Err(); err != nil {
			log.Errorf("GPS scanner error: %v", err)
		}

		port.Close()
		log.Infof("GPS reader stopped, reconnecting in 10s...")
		time.Sleep(10 * time.Second)
	}
}

var lastWifiRestart time.Time

func restartWifi(cmd *cobra.Command) error {
	restartRetryTime := viper.GetDuration("wifi_restart_retry_time")

	if time.Now().Sub(lastWifiRestart) < restartRetryTime {
		return nil
	}
	defer func() {
		lastWifiRestart = time.Now()
	}()

	restartCommand := viper.GetString("wifi_restart_command")
	if restartCommand == "" {
		log.Debugf("wifi restart disabled")
		return nil
	}

	log.Debugf("Attempting to restart wifi")

	restartCmd := exec.Command("/bin/sh", "-c", restartCommand)

	stdoutStderr, err := restartCmd.CombinedOutput()
	if len(stdoutStderr) > 0 {
		log.Infof("Output from wifi restart: %s", stdoutStderr)
	}
	return err
}

type mqttClient struct {
	client         mqtt.Client
	options        *mqtt.ClientOptions
	mqttData       map[string]string
	updateInterval time.Duration

	phev        *client.Client
	lastConnect time.Time
	lastError   error

	prefix string

	haDiscovery          bool
	haDiscoveryPrefix    string
	haPublishedDiscovery bool

	climate *climate
	gps     *gpsLocation
	enabled bool
}

func (m *mqttClient) topic(topic string) string {
	return fmt.Sprintf("%s%s", m.prefix, topic)
}

func (m *mqttClient) Run(cmd *cobra.Command, args []string) error {
	m.enabled = true // Default.

	mqttServer := viper.GetString("mqtt_server")
	mqttUsername := viper.GetString("mqtt_username")
	mqttPassword := viper.GetString("mqtt_password")
	mqttDisableSet := viper.GetBool("mqtt_disable_register_set_command")
	m.prefix = viper.GetString("mqtt_topic_prefix")
	m.haDiscovery = viper.GetBool("ha_discovery")
	m.haDiscoveryPrefix = viper.GetString("ha_discovery_prefix")
	m.updateInterval = viper.GetDuration("update_interval")
	wifiRestartTime := viper.GetDuration("wifi_restart_time")
	restartCommand := viper.GetString("wifi_restart_command")

	if restartCommand == "" {
		log.Infof("WiFi restart disabled")
	}

	m.haPublishedDiscovery = false
	m.lastError = nil

	m.options = mqtt.NewClientOptions().
		AddBroker(mqttServer).
		SetClientID("phev2mqtt").
		SetUsername(mqttUsername).
		SetPassword(mqttPassword).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(10*time.Second).
		SetDefaultPublishHandler(m.handleIncomingMqtt).
		SetConnectionLostHandler(func(client mqtt.Client, err error) {
			log.Errorf("MQTT connection lost: %v. Reconnecting in 10 seconds...", err)
		}).
		SetOnConnectHandler(func(client mqtt.Client) {
			log.Infof("MQTT connected successfully")
		}).
		SetWill(m.topic("/available"), "offline", 0, true)

	m.client = mqtt.NewClient(m.options)
	if token := m.client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	if !mqttDisableSet {
		if token := m.client.Subscribe(m.topic("/set/#"), 0, nil); token.Wait() && token.Error() != nil {
			return token.Error()
		}
	} else {
		log.Info("Setting vechicle registers via MQTT is disabled")
	}
	if token := m.client.Subscribe(m.topic("/connection"), 0, nil); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	if token := m.client.Subscribe(m.topic("/settings/#"), 0, nil); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	m.mqttData = map[string]string{}

	// Initialize GPS
	m.gps = &gpsLocation{
		enabled: true,
	}

	// Start GPS reader in background
	go m.readGPS(m.gps)
	log.Infof("GPS reader initialized (disabled by default, enable via MQTT /set/gps)")

	for {
		if m.enabled {
			if err := m.handlePhev(cmd); err != nil {
				// Do not flood the log with the same messages every second
				if m.lastError == nil || m.lastError.Error() != err.Error() {
					log.Error(err)
					m.lastError = err
				}
			}
			// Publish as offline if last connection was >30s ago.
			if time.Now().Sub(m.lastConnect) > 30*time.Second {
				m.client.Publish(m.topic("/available"), 0, true, "offline")
			}
			// Restart Wifi interface if > wifi_restart_time.
			if wifiRestartTime > 0 && time.Now().Sub(m.lastConnect) > wifiRestartTime {
				if err := restartWifi(cmd); err != nil {
					log.Errorf("Error restarting wifi: %v", err)
				}
			}
		}

		time.Sleep(time.Second)
	}
}

func (m *mqttClient) publish(topic, payload string) {
	//	if cache := m.mqttData[topic]; cache != payload {
	m.client.Publish(m.topic(topic), 0, false, payload)
	m.mqttData[topic] = payload
	// }
}

func (m *mqttClient) publishSystemMetrics() {
	metrics := collectSystemMetrics()

	// Publish CPU temperature
	if metrics.CPUTemp > 0 {
		m.publish("/system/cpu_temp", fmt.Sprintf("%.1f", metrics.CPUTemp))
	}

	// Publish memory usage
	if metrics.MemoryPercent > 0 {
		m.publish("/system/memory_percent", fmt.Sprintf("%.1f", metrics.MemoryPercent))
	}

	// Publish CPU load
	if metrics.CPULoad >= 0 {
		m.publish("/system/cpu_load", fmt.Sprintf("%.1f", metrics.CPULoad))
	}

	// Publish disk usage
	if metrics.DiskPercent > 0 {
		m.publish("/system/disk_percent", fmt.Sprintf("%.1f", metrics.DiskPercent))
	}

	// Publish uptime
	if metrics.UptimeSeconds > 0 {
		m.publish("/system/uptime", fmt.Sprintf("%d", metrics.UptimeSeconds))
	}
}

func (m *mqttClient) publishGPS(gps *gpsLocation) {
	data := gps.getData()

	// Publish individual GPS data
	m.publish("/gps/latitude", fmt.Sprintf("%.6f", data["latitude"].(float64)))
	m.publish("/gps/longitude", fmt.Sprintf("%.6f", data["longitude"].(float64)))
	m.publish("/gps/altitude", fmt.Sprintf("%.1f", data["altitude"].(float64)))
	m.publish("/gps/speed", fmt.Sprintf("%.1f", data["speed"].(float64)))
	m.publish("/gps/heading", fmt.Sprintf("%.1f", data["heading"].(float64)))
	m.publish("/gps/satellites", fmt.Sprintf("%d", data["satellites"].(int)))
	m.publish("/gps/fix_quality", fmt.Sprintf("%d", data["fix_quality"].(int)))

	// Publish combined location for device_tracker (HA format)
	locationJSON := fmt.Sprintf(`{"latitude":%.6f,"longitude":%.6f,"gps_accuracy":10,"speed":%.1f,"altitude":%.1f,"course":%.1f}`,
		data["latitude"].(float64),
		data["longitude"].(float64),
		data["speed"].(float64),
		data["altitude"].(float64),
		data["heading"].(float64))

	m.publish("/gps/location", locationJSON)

	log.Debugf("GPS published: lat=%.6f lon=%.6f speed=%.1f km/h sats=%d",
		data["latitude"].(float64),
		data["longitude"].(float64),
		data["speed"].(float64),
		data["satellites"].(int))
}

func (m *mqttClient) handleIncomingMqtt(mqtt_client mqtt.Client, msg mqtt.Message) {
	log.Infof("Topic: [%s] Payload: [%s]", msg.Topic(), msg.Payload())

	topicParts := strings.Split(msg.Topic(), "/")
	if strings.HasPrefix(msg.Topic(), m.topic("/set/register/")) {
		if len(topicParts) != 4 {
			log.Infof("Bad topic format [%s]", msg.Topic())
			return
		}
		register, err := hex.DecodeString(topicParts[3])
		if err != nil {
			log.Infof("Bad register in topic [%s]: %v", msg.Topic(), err)
			return
		}
		data, err := hex.DecodeString(string(msg.Payload()))
		if err != nil {
			log.Infof("Bad payload [%s]: %v", msg.Payload(), err)
			return
		}
		if err := m.phev.SetRegister(register[0], data); err != nil {
			log.Infof("Error setting register %02x: %v", register[0], err)
			return
		}
	} else if msg.Topic() == m.topic("/connection") {
		payload := strings.ToLower(string(msg.Payload()))
		switch payload {
		case "off":
			m.enabled = false
			m.phev.Close()
			m.client.Publish(m.topic("/available"), 0, true, "offline")
		case "on":
			m.enabled = true
		case "restart":
			m.enabled = true
			m.client.Publish(m.topic("/available"), 0, true, "offline")
			m.phev.Close()
		}
	} else if msg.Topic() == m.topic("/set/parkinglights") {
		values := map[string]byte{"on": 0x1, "off": 0x2}
		if v, ok := values[strings.ToLower(string(msg.Payload()))]; ok {
			if err := m.phev.SetRegister(0xb, []byte{v}); err != nil {
				log.Infof("Error setting register 0xb: %v", err)
				return
			}
		}
	} else if msg.Topic() == m.topic("/set/headlights") {
		values := map[string]byte{"on": 0x1, "off": 0x2}
		if v, ok := values[strings.ToLower(string(msg.Payload()))]; ok {
			if err := m.phev.SetRegister(0xa, []byte{v}); err != nil {
				log.Infof("Error setting register 0xb: %v", err)
				return
			}
		}
	} else if msg.Topic() == m.topic("/set/cancelchargetimer") {
		if err := m.phev.SetRegister(0x17, []byte{0x1}); err != nil {
			log.Infof("Error setting register 0x17: %v", err)
			return
		}
		if err := m.phev.SetRegister(0x17, []byte{0x11}); err != nil {
			log.Infof("Error setting register 0x17: %v", err)
			return
		}
	} else if strings.HasPrefix(msg.Topic(), m.topic("/set/climate/state")) {
		payload := strings.ToLower(string(msg.Payload()))
		if payload == "reset" {
			if err := m.phev.SetRegister(protocol.SetAckPreACTermRegister, []byte{0x1}); err != nil {
				log.Infof("Error acknowledging Pre-AC termination: %v", err)
				return
			}
		}
	} else if strings.HasPrefix(msg.Topic(), m.topic("/set/climate/")) {
		topic := msg.Topic()
		payload := strings.ToLower(string(msg.Payload()))

		modeMap := map[string]byte{"off": 0x0, "OFF": 0x0, "cool": 0x1, "heat": 0x2, "windscreen": 0x3, "mode": 0x4}
		durMap := map[string]byte{"10": 0x0, "20": 0x1, "30": 0x2, "on": 0x0, "off": 0x0}
		parts := strings.Split(topic, "/")
		mode, ok := modeMap[parts[len(parts)-1]]
		if !ok {
			log.Errorf("Unknown climate mode: %s", parts[len(parts)-1])
			return
		}
		if mode == 0x4 { // set/climate/mode -> "heat"
			mode = modeMap[payload]
			payload = "on"
		}
		if payload == "off" {
			mode = 0x0
		}
		duration, ok := durMap[payload]
		if mode != 0x0 && !ok {
			log.Errorf("Unknown climate duration: %s", payload)
			return
		}

		if m.phev.ModelYear == client.ModelYear14 {
			// MY2014 uses different approach - based on GitHub issue #11
			// Use register 0x05 for climate timer setting approach
			log.Debugf("Setting MY2014 climate mode: %d, duration: %d", mode, duration)

			// First, try the climate timer register approach (0x05)
			// This mimics what the official app does according to the GitHub issue
			timerPayload := make([]byte, 16)
			if mode != 0x0 {
				// Encode the climate setting in timer format
				// Based on protocol docs: Timer encoding with immediate activation
				timerPayload[0] = 0x01            // Enable immediate
				timerPayload[1] = mode | duration // Mode and duration combined
			}

			if err := m.phev.SetRegister(0x05, timerPayload); err != nil {
				log.Infof("Error setting climate timer (0x05): %v", err)
				// Fallback to original method if timer method fails
				registerPayload := bytes.Repeat([]byte{0xff}, 15)
				registerPayload[0] = 0x0
				registerPayload[1] = 0x0
				registerPayload[6] = mode | duration
				if err := m.phev.SetRegister(protocol.SetACModeRegisterMY14, registerPayload); err != nil {
					log.Infof("Error setting AC mode (fallback): %v", err)
					return
				}
			}

			// Enable/disable the AC
			acEnabled := byte(0x02)
			if mode == 0x0 {
				acEnabled = 0x01
			}
			if err := m.phev.SetRegister(protocol.SetACEnabledRegisterMY14, []byte{acEnabled}); err != nil {
				log.Infof("Error setting AC enabled state: %v", err)
				return
			}
		} else if m.phev.ModelYear == client.ModelYear18 || m.phev.ModelYear == client.ModelYear24 {
			state := byte(0x02)
			if mode == 0x0 {
				state = 0x1
			}
			if err := m.phev.SetRegister(protocol.SetACModeRegisterMY18, []byte{state, mode, duration, 0x0}); err != nil {
				log.Infof("Error setting AC mode: %v", err)
				return
			}
		}
	} else if strings.HasPrefix(msg.Topic(), m.topic("/set/climate/timer/")) {
		// Handle climate timer setting: /set/climate/timer/[1-5]
		// Expected payload: JSON format
		// Example: {"enabled":true,"hour":7,"minute":30,"mode":"heat","duration":20,"days":["mon","tue","wed","thu","fri"]}

		parts := strings.Split(msg.Topic(), "/")
		if len(parts) < 6 {
			log.Errorf("Invalid climate timer topic: %s", msg.Topic())
			return
		}

		timerIdStr := parts[len(parts)-1]
		timerId, err := strconv.Atoi(timerIdStr)
		if err != nil || timerId < 1 || timerId > 5 {
			log.Errorf("Invalid timer ID: %s (must be 1-5)", timerIdStr)
			return
		}

		// Parse JSON payload
		var timerConfig protocol.ClimateTimer
		if err := json.Unmarshal(msg.Payload(), &timerConfig); err != nil {
			log.Errorf("Invalid JSON payload for climate timer: %v", err)
			return
		}

		// Validate timer configuration
		if timerConfig.Enabled {
			if timerConfig.Hour > 23 || timerConfig.Minute > 50 || timerConfig.Minute%10 != 0 {
				log.Errorf("Invalid time: %02d:%02d (hour 0-23, minute 0,10,20,30,40,50)", timerConfig.Hour, timerConfig.Minute)
				return
			}
			if timerConfig.Duration != 10 && timerConfig.Duration != 20 && timerConfig.Duration != 30 {
				log.Errorf("Invalid duration: %d (must be 10, 20, or 30 minutes)", timerConfig.Duration)
				return
			}
			if timerConfig.Mode != "cool" && timerConfig.Mode != "heat" && timerConfig.Mode != "windscreen" {
				log.Errorf("Invalid mode: %s (must be cool, heat, or windscreen)", timerConfig.Mode)
				return
			}
		}

		// Create climate timer register data
		climateReg := &protocol.RegisterClimateTimer{}

		// Set all timers to disabled first (we'll get current state in real implementation)
		for i := 0; i < 5; i++ {
			climateReg.Timers[i].Enabled = false
		}

		// Set the specific timer
		climateReg.Timers[timerId-1] = timerConfig

		// Send the timer setting
		msg := climateReg.Encode()
		msg.Type = protocol.CmdOutSend
		msg.Register = protocol.SetClimateTimerRegister // Use 0x1a for setting

		if err := m.phev.SetRegister(protocol.SetClimateTimerRegister, msg.Data); err != nil {
			log.Errorf("Error setting climate timer %d: %v", timerId, err)
			return
		}

		log.Infof("Climate timer %d set successfully", timerId)

	} else if msg.Topic() == m.topic("/set/climate/timer/clear") {
		// Clear all climate timers
		clearData := make([]byte, 16)
		clearData[0] = 0x00
		for i := 1; i < 16; i += 3 {
			clearData[i] = 0xfe
			clearData[i+1] = 0x07
			clearData[i+2] = 0x00
		}
		clearData[15] = 0x01

		if err := m.phev.SetRegister(protocol.SetClimateTimerRegister, clearData); err != nil {
			log.Errorf("Error clearing climate timers: %v", err)
			return
		}

		log.Infof("All climate timers cleared")

	} else if msg.Topic() == m.topic("/set/climate/start") {
		// Start climate control immediately using timer system
		// Expected payload: JSON format
		// Example: {"mode":"heat","duration":20}
		// This creates a "now" timer that activates immediately

		var startConfig struct {
			Mode     string `json:"mode"`
			Duration uint8  `json:"duration"`
		}

		if err := json.Unmarshal(msg.Payload(), &startConfig); err != nil {
			log.Errorf("Invalid JSON payload for climate start: %v", err)
			return
		}

		// Validate configuration
		if startConfig.Mode != "cool" && startConfig.Mode != "heat" && startConfig.Mode != "windscreen" {
			log.Errorf("Invalid mode: %s (must be cool, heat, or windscreen)", startConfig.Mode)
			return
		}
		if startConfig.Duration != 10 && startConfig.Duration != 20 && startConfig.Duration != 30 {
			log.Errorf("Invalid duration: %d (must be 10, 20, or 30 minutes)", startConfig.Duration)
			return
		}

		// Create immediate timer - use current time
		now := time.Now()
		immediateTimer := protocol.ClimateTimer{
			Enabled:  true,
			Hour:     uint8(now.Hour()),
			Minute:   uint8((now.Minute() / 10) * 10), // Round to nearest 10 minutes
			Mode:     startConfig.Mode,
			Duration: startConfig.Duration,
			Days:     []string{}, // Empty days means "now"
		}

		// Create timer register with immediate activation
		climateReg := &protocol.RegisterClimateTimer{}
		// Set all timers to disabled first
		for i := 0; i < 5; i++ {
			climateReg.Timers[i].Enabled = false
		}

		// Use timer slot 1 for immediate activation
		climateReg.Timers[0] = immediateTimer

		// Send the immediate timer
		msg := climateReg.Encode()
		if err := m.phev.SetRegister(protocol.SetClimateTimerRegister, msg.Data); err != nil {
			log.Errorf("Error starting immediate climate control: %v", err)
			return
		}

		log.Infof("Climate control started immediately: %s for %d minutes", startConfig.Mode, startConfig.Duration)

	} else if msg.Topic() == m.topic("/set/climate/stop") {
		// Stop climate control immediately
		// This sends the termination/reset command

		if err := m.phev.SetRegister(protocol.SetAckPreACTermRegister, []byte{0x1}); err != nil {
			log.Errorf("Error stopping climate control: %v", err)
			return
		}

		log.Infof("Climate control stopped")

	} else if msg.Topic() == m.topic("/set/climate/now") {
		// Direct immediate climate control (alternative method)
		// Expected payload: "heat:20", "cool:10", "windscreen:30", "off"

		payload := strings.ToLower(string(msg.Payload()))

		if payload == "off" {
			// Stop climate control
			if err := m.phev.SetRegister(protocol.SetAckPreACTermRegister, []byte{0x1}); err != nil {
				log.Errorf("Error stopping climate control: %v", err)
				return
			}
			log.Infof("Climate control turned off")
			return
		}

		parts := strings.Split(payload, ":")
		if len(parts) != 2 {
			log.Errorf("Invalid payload format. Expected 'mode:duration' (e.g., 'heat:20') or 'off'")
			return
		}

		mode := parts[0]
		durationStr := parts[1]

		// Validate mode
		modeMap := map[string]byte{"cool": 0x1, "heat": 0x2, "windscreen": 0x3}
		modeCode, ok := modeMap[mode]
		if !ok {
			log.Errorf("Invalid mode: %s (must be cool, heat, or windscreen)", mode)
			return
		}

		// Validate duration
		durMap := map[string]byte{"10": 0x0, "20": 0x1, "30": 0x2}
		durationCode, ok := durMap[durationStr]
		if !ok {
			log.Errorf("Invalid duration: %s (must be 10, 20, or 30)", durationStr)
			return
		}

		// Use the enhanced MY2014 approach for immediate activation
		if m.phev.ModelYear == client.ModelYear14 {
			// Use register 0x05 approach for MY2014
			timerPayload := make([]byte, 16)
			timerPayload[0] = 0x01                           // Enable immediate
			timerPayload[1] = modeCode | (durationCode << 4) // Mode and duration combined
			// Set current time for immediate activation
			now := time.Now()
			// Encode immediate timer in first timer slot (bytes 1-3)
			immediateTimer := protocol.ClimateTimer{
				Enabled:  true,
				Hour:     uint8(now.Hour()),
				Minute:   uint8((now.Minute() / 10) * 10),
				Mode:     mode,
				Duration: uint8((durationCode + 1) * 10),
				Days:     []string{}, // No repeat days for immediate activation
			}
			timer24bit := protocol.EncodeClimateTimer(immediateTimer)
			timerPayload[1] = byte(timer24bit >> 16)
			timerPayload[2] = byte(timer24bit >> 8)
			timerPayload[3] = byte(timer24bit)

			if err := m.phev.SetRegister(0x05, timerPayload); err != nil {
				log.Errorf("Error starting immediate climate (MY2014): %v", err)
				return
			}
		} else {
			// Use register 0x1b for MY18+
			state := byte(0x02)
			if err := m.phev.SetRegister(protocol.SetACModeRegisterMY18, []byte{state, modeCode, durationCode, 0x0}); err != nil {
				log.Errorf("Error starting immediate climate: %v", err)
				return
			}
		}

		log.Infof("Climate control started: %s for %s minutes", mode, durationStr)

	} else if msg.Topic() == m.topic("/set/gps") {
		// Enable/disable GPS tracking
		payload := strings.ToLower(string(msg.Payload()))

		if payload == "on" {
			m.gps.setEnabled(true)
			log.Infof("GPS tracking enabled")
			m.publish("/gps/status", "on")
		} else if payload == "off" {
			m.gps.setEnabled(false)
			log.Infof("GPS tracking disabled")
			m.publish("/gps/status", "off")
		} else {
			log.Errorf("Invalid GPS command: %s (must be 'on' or 'off')", payload)
		}

	} else if msg.Topic() == m.topic("/set/system/reboot") {
		// Reboot the Raspberry Pi
		log.Warnf("Reboot requested via MQTT, rebooting system in 5 seconds...")
		m.publish("/system/status", "rebooting")

		// Use a goroutine to allow the MQTT message to be sent before rebooting
		go func() {
			time.Sleep(5 * time.Second)
			cmd := exec.Command("sudo", "reboot")
			if err := cmd.Run(); err != nil {
				log.Errorf("Failed to reboot: %v", err)
				m.publish("/system/status", "reboot_failed")
			}
		}()

	} else if msg.Topic() == m.topic("/settings/dump") {
		log.Infof("CURRENT_SETTINGS:")
		log.Infof("\n%s", m.phev.Settings.Dump())
		m.phev.Settings.Clear()
	} else {
		log.Errorf("Unknown topic from mqtt: %s", msg.Topic())
	}
}

func (m *mqttClient) handlePhev(cmd *cobra.Command) error {
	var err error
	address := viper.GetString("address")
	m.phev, err = client.New(client.AddressOption(address))
	if err != nil {
		return err
	}

	if err := m.phev.Connect(); err != nil {
		return err
	}

	if err := m.phev.Start(); err != nil {
		return err
	}
	m.client.Publish(m.topic("/available"), 0, true, "online")

	m.lastError = nil

	defer func() {
		m.lastConnect = time.Now()
	}()

	var encodingErrorCount = 0
	var lastEncodingError time.Time

	updaterTicker := time.NewTicker(m.updateInterval)
	for {
		select {
		case <-updaterTicker.C:
			m.phev.SetRegister(0x6, []byte{0x3})
			// Collect and publish system metrics
			m.publishSystemMetrics()
		case msg, ok := <-m.phev.Recv:
			if !ok {
				log.Infof("Connection closed.")
				updaterTicker.Stop()
				return fmt.Errorf("Connection closed.")
			}
			switch msg.Type {
			case protocol.CmdInBadEncoding:
				if time.Now().Sub(lastEncodingError) > 15*time.Second {
					encodingErrorCount = 0
				}
				if encodingErrorCount > 50 {
					m.phev.Close()
					updaterTicker.Stop()
					return fmt.Errorf("Disconnecting due to too many errors")
				}
				encodingErrorCount += 1
				lastEncodingError = time.Now()
			case protocol.CmdInResp:
				if msg.Ack != protocol.Request {
					break
				}
				m.publishRegister(msg)
				m.phev.Send <- &protocol.PhevMessage{
					Type:     protocol.CmdOutSend,
					Register: msg.Register,
					Ack:      protocol.Ack,
					Xor:      msg.Xor,
					Data:     []byte{0x0},
				}
			}
		}
	}
}

var boolOnOff = map[bool]string{
	false: "off",
	true:  "on",
}
var boolOpen = map[bool]string{
	false: "closed",
	true:  "open",
}

func (m *mqttClient) publishRegister(msg *protocol.PhevMessage) {
	dataStr := hex.EncodeToString(msg.Data)
	m.publish(fmt.Sprintf("/register/%02x", msg.Register), dataStr)
	switch reg := msg.Reg.(type) {
	case *protocol.RegisterVIN:
		m.publish("/vin", reg.VIN)
		m.publishHomeAssistantDiscovery(reg.VIN, m.prefix, "Phev")
		m.publish("/registrations", fmt.Sprintf("%d", reg.Registrations))
	case *protocol.RegisterECUVersion:
		m.publish("/ecuversion", reg.Version)
	case *protocol.RegisterACMode:
		m.climate.setMode(reg.Mode)
		for t, p := range m.climate.mqttStates() {
			m.publish(t, p)
		}
	case *protocol.RegisterClimateTimer:
		// Publish climate timer status
		for i, timer := range reg.Timers {
			timerJSON, err := json.Marshal(timer)
			if err != nil {
				log.Errorf("Error marshaling climate timer %d: %v", i+1, err)
				continue
			}
			m.publish(fmt.Sprintf("/climate/timer/%d", i+1), string(timerJSON))
		}
	case *protocol.RegisterPreACState:
		m.climate.setState(reg.State)
		for t, p := range m.climate.mqttStates() {
			m.publish(t, p)
		}
	case *protocol.RegisterChargeStatus:
		m.publish("/charge/charging", boolOnOff[reg.Charging])
		if reg.Remaining < 1000 {
			m.publish("/charge/remaining", fmt.Sprintf("%d", reg.Remaining))
		} else {
			log.Debugf("Ignoring charge remanining reading: %v", reg.Remaining)
			if cache := m.mqttData["/charge/remaining"]; cache != "" {
				m.publish("/charge/remaining", cache)
				log.Debugf("Publishing last best known charge remaining reading: %v", cache)
			}
		}
	case *protocol.RegisterDoorStatus:
		m.publish("/door/locked", boolOpen[!reg.Locked])
		m.publish("/door/rear_left", boolOpen[reg.RearLeft])
		m.publish("/door/rear_right", boolOpen[reg.RearRight])
		m.publish("/door/front_right", boolOpen[reg.Driver])
		m.publish("/door/driver", boolOpen[reg.Driver])
		m.publish("/door/front_left", boolOpen[reg.FrontPassenger])
		m.publish("/door/front_passenger", boolOpen[reg.FrontPassenger])
		m.publish("/door/bonnet", boolOpen[reg.Bonnet])
		m.publish("/door/boot", boolOpen[reg.Boot])
		m.publish("/lights/head", boolOnOff[reg.Headlights])
	case *protocol.RegisterBatteryLevel:
		if (reg.Level > 5) && (reg.Level < 255) {
			m.publish("/battery/level", fmt.Sprintf("%d", reg.Level))
		} else {
			if cache := m.mqttData["/battery/level"]; cache != "" {
				m.publish("/battery/level", cache)
				log.Debugf("Ignoring battery level reading: %v, publishing last best known: %v", reg.Level, cache)
			}
		}
		m.publish("/lights/parking", boolOnOff[reg.ParkingLights])
	case *protocol.RegisterLightStatus:
		m.publish("/lights/interior", boolOnOff[reg.Interior])
		m.publish("/lights/hazard", boolOnOff[reg.Hazard])
	case *protocol.RegisterChargePlug:
		if reg.Connected {
			m.publish("/charge/plug", "connected")
		} else {
			m.publish("/charge/plug", "unplugged")
		}
	}
}

// Publish home assistant discovery message.
// Uses the vehicle VIN, so sent after VIN discovery.
func (m *mqttClient) publishHomeAssistantDiscovery(vin, topic, name string) {

	if m.haPublishedDiscovery || !m.haDiscovery {
		return
	}
	m.haPublishedDiscovery = true
	discoveryData := map[string]string{
		// Doors.
		"%s/binary_sensor/%s_door_locked/config": `{
		"device_class": "lock",
		"name": "__NAME__ Locked",
		"state_topic": "~/door/locked",
		"payload_off": "closed",
		"payload_on": "open",
		"avty_t": "~/available",
		"unique_id": "__VIN___door_locked",
		"device": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"},
		"~": "__TOPIC__"}`,
		"%s/binary_sensor/%s_door_bonnet/config": `{
		"device_class": "door",
		"name": "__NAME__ Bonnet",
		"state_topic": "~/door/bonnet",
		"payload_off": "closed",
		"payload_on": "open",
		"avty_t": "~/available",
		"unique_id": "__VIN___door_bonnet",
		"device": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"},
		"~": "__TOPIC__"}`,
		"%s/binary_sensor/%s_door_boot/config": `{
		"device_class": "door",
		"name": "__NAME__ Boot",
		"state_topic": "~/door/boot",
		"payload_off": "closed",
		"payload_on": "open",
		"avty_t": "~/available",
		"unique_id": "__VIN___door_boot",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/binary_sensor/%s_door_front_passenger/config": `{
		"device_class": "door",
		"name": "__NAME__ Front Passenger Door",
		"state_topic": "~/door/front_passenger",
		"payload_off": "closed",
		"payload_on": "open",
		"avty_t": "~/available",
		"unique_id": "__VIN___door_front_passenger",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/binary_sensor/%s_door_driver/config": `{
		"device_class": "door",
		"name": "__NAME__ Driver Door",
		"state_topic": "~/door/driver",
		"payload_off": "closed",
		"payload_on": "open",
		"avty_t": "~/available",
		"unique_id": "__VIN___door_driver",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/binary_sensor/%s_door_rear_left/config": `{
		"device_class": "door",
		"name": "__NAME__ Rear Left Door",
		"state_topic": "~/door/rear_left",
		"payload_off": "closed",
		"payload_on": "open",
		"avty_t": "~/available",
		"unique_id": "__VIN___door_rear_left",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/binary_sensor/%s_door_rear_right/config": `{
		"device_class": "door",
		"name": "__NAME__ Rear Right Door",
		"state_topic": "~/door/rear_right",
		"payload_off": "closed",
		"payload_on": "open",
		"avty_t": "~/available",
		"unique_id": "__VIN___door_rear_right",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,

		// Battery and charging
		"%s/sensor/%s_battery_level/config": `{
		"device_class": "battery",
		"name": "__NAME__ Battery",
		"state_topic": "~/battery/level",
		"state_class": "measurement",
		"unit_of_measurement": "%",
		"avty_t": "~/available",
		"unique_id": "__VIN___battery_level",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_battery_charge_remaining/config": `{
		"name": "__NAME__ Charge Remaining",
		"state_topic": "~/charge/remaining",
		"unit_of_measurement": "min",
		"avty_t": "~/available",
		"unique_id": "__VIN___battery_charge_remaining",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/binary_sensor/%s_charger_connected/config": `{
		"device_class": "plug",
		"name": "__NAME__ Charger Connected",
		"state_topic": "~/charge/plug",
		"payload_on": "connected",
		"payload_off": "unplugged",
		"avty_t": "~/available",
		"unique_id": "__VIN___charger_connected",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/binary_sensor/%s_battery_charging/config": `{
		"device_class": "battery_charging",
		"name": "__NAME__ Charging",
		"state_topic": "~/charge/charging",
		"payload_on": "on",
		"payload_off": "off",
		"avty_t": "~/available",
		"unique_id": "__VIN___battery_charging",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/switch/%s_cancel_charge_timer/config": `{
		"name": "__NAME__ Disable Charge Timer",
		"icon": "mdi:timer-off",
		"state_topic": "~/battery/charging",
		"command_topic": "~/set/cancelchargetimer",
		"avty_t": "~/available",
		"unique_id": "__VIN___cancel_charge_timer",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		// Climate
		"%s/switch/%s_climate_heat/config": `{
		"name": "__NAME__ Heat",
		"icon": "mdi:weather-sunny",
		"state_topic": "~/climate/heat",
		"command_topic": "~/set/climate/heat",
		"payload_off": "off",
		"payload_on": "on",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_heat",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/switch/%s_climate_cool/config": `{
		"name": "__NAME__ cool",
		"icon": "mdi:air-conditioner",
		"state_topic": "~/climate/cool",
		"command_topic": "~/set/climate/cool",
		"payload_off": "off",
		"payload_on": "on",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_cool",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/switch/%s_climate_windscreen/config": `{
		"name": "__NAME__ windscreen",
		"state_topic": "~/climate/windscreen",
		"command_topic": "~/set/climate/windscreen",
		"payload_off": "off",
		"payload_on": "on",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_windscreen",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"icon": "mdi:car-defrost-front",
		"~": "__TOPIC__"}`,
		"%s/select/%s_climate_on/config": `{
				"name": "__NAME__ climate state",
				"state_topic": "~/climate/mode",
				"command_topic": "~/set/climate/mode",
				"options": [ "off", "heat", "cool", "windscreen"],
				"avty_t": "~/available",
				"unique_id": "__VIN___climate_on",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
				"icon": "mdi:car-seat-heater",
				"~": "__TOPIC__"}`,
		"%s/button/%s_climate_stop/config": `{
		"name": "__NAME__ Stop Climate",
		"icon": "mdi:stop-circle",
		"command_topic": "~/set/climate/stop",
		"payload_press": "",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_stop",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_climate_timer_1/config": `{
		"name": "__NAME__ Climate Timer 1",
		"icon": "mdi:timer",
		"state_topic": "~/climate/timer/1",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_timer_1",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_climate_timer_2/config": `{
		"name": "__NAME__ Climate Timer 2",
		"icon": "mdi:timer",
		"state_topic": "~/climate/timer/2",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_timer_2",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_climate_timer_3/config": `{
		"name": "__NAME__ Climate Timer 3",
		"icon": "mdi:timer",
		"state_topic": "~/climate/timer/3",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_timer_3",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_climate_timer_4/config": `{
		"name": "__NAME__ Climate Timer 4",
		"icon": "mdi:timer",
		"state_topic": "~/climate/timer/4",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_timer_4",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_climate_timer_5/config": `{
		"name": "__NAME__ Climate Timer 5",
		"icon": "mdi:timer",
		"state_topic": "~/climate/timer/5",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_timer_5",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_clear_timers/config": `{
		"name": "__NAME__ Clear All Climate Timers",
		"icon": "mdi:timer-off",
		"command_topic": "~/set/climate/timer/clear",
		"payload_press": "",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_clear_timers",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_heat_10/config": `{
		"name": "__NAME__ Heat 10 min",
		"icon": "mdi:fire",
		"command_topic": "~/set/climate/now",
		"payload_press": "heat:10",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_heat_10",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_heat_20/config": `{
		"name": "__NAME__ Heat 20 min",
		"icon": "mdi:fire",
		"command_topic": "~/set/climate/now",
		"payload_press": "heat:20",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_heat_20",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_heat_30/config": `{
		"name": "__NAME__ Heat 30 min",
		"icon": "mdi:fire",
		"command_topic": "~/set/climate/now",
		"payload_press": "heat:30",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_heat_30",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_cool_10/config": `{
		"name": "__NAME__ Cool 10 min",
		"icon": "mdi:snowflake",
		"command_topic": "~/set/climate/now",
		"payload_press": "cool:10",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_cool_10",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_cool_20/config": `{
		"name": "__NAME__ Cool 20 min",
		"icon": "mdi:snowflake",
		"command_topic": "~/set/climate/now",
		"payload_press": "cool:20",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_cool_20",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_cool_30/config": `{
		"name": "__NAME__ Cool 30 min",
		"icon": "mdi:snowflake",
		"command_topic": "~/set/climate/now",
		"payload_press": "cool:30",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_cool_30",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_windscreen_10/config": `{
		"name": "__NAME__ Windscreen 10 min",
		"icon": "mdi:car-defrost-front",
		"command_topic": "~/set/climate/now",
		"payload_press": "windscreen:10",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_windscreen_10",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_windscreen_20/config": `{
		"name": "__NAME__ Windscreen 20 min",
		"icon": "mdi:car-defrost-front",
		"command_topic": "~/set/climate/now",
		"payload_press": "windscreen:20",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_windscreen_20",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_climate_windscreen_30/config": `{
		"name": "__NAME__ Windscreen 30 min",
		"icon": "mdi:car-defrost-front",
		"command_topic": "~/set/climate/now",
		"payload_press": "windscreen:30",
		"avty_t": "~/available",
		"unique_id": "__VIN___climate_windscreen_30",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		// Lights.
		"%s/light/%s_parkinglights/config": `{
		"name": "__NAME__ Park Lights",
		"icon": "mdi:car-parking-lights",
		"state_topic": "~/lights/parking",
		"command_topic": "~/set/parkinglights",
		"payload_off": "off",
		"payload_on": "on",
		"avty_t": "~/available",
		"unique_id": "__VIN___parkinglights",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/light/%s_headlights/config": `{
		"name": "__NAME__ Head Lights",
		"icon": "mdi:car-light-high",
		"state_topic": "~/lights/head",
		"command_topic": "~/set/headlights",
		"payload_off": "off",
		"payload_on": "on",
		"avty_t": "~/available",
		"unique_id": "__VIN___headlights",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		// General topics.
		"%s/button/%s_reconnect_wifi/config": `{
		"name": "__NAME__ Restart Wifi connetion",
		"icon": "mdi:timer-off",
		"command_topic": "~/connection",
		"payload_press": "restart",
		"avty_t": "~/available",
		"unique_id": "__VIN___restart_wifi",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/button/%s_system_reboot/config": `{
		"name": "__NAME__ Reboot System",
		"icon": "mdi:restart",
		"command_topic": "~/set/system/reboot",
		"payload_press": "",
		"avty_t": "~/available",
		"unique_id": "__VIN___system_reboot",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		// System monitoring
		"%s/sensor/%s_system_cpu_temp/config": `{
		"name": "__NAME__ CPU Temperature",
		"icon": "mdi:thermometer",
		"device_class": "temperature",
		"state_topic": "~/system/cpu_temp",
		"unit_of_measurement": "°C",
		"state_class": "measurement",
		"avty_t": "~/available",
		"unique_id": "__VIN___system_cpu_temp",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_system_memory/config": `{
		"name": "__NAME__ Memory Usage",
		"icon": "mdi:memory",
		"state_topic": "~/system/memory_percent",
		"unit_of_measurement": "%",
		"state_class": "measurement",
		"avty_t": "~/available",
		"unique_id": "__VIN___system_memory",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_system_cpu_load/config": `{
		"name": "__NAME__ CPU Load",
		"icon": "mdi:chip",
		"state_topic": "~/system/cpu_load",
		"unit_of_measurement": "%",
		"state_class": "measurement",
		"avty_t": "~/available",
		"unique_id": "__VIN___system_cpu_load",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_system_disk/config": `{
		"name": "__NAME__ Disk Usage",
		"icon": "mdi:harddisk",
		"state_topic": "~/system/disk_percent",
		"unit_of_measurement": "%",
		"state_class": "measurement",
		"avty_t": "~/available",
		"unique_id": "__VIN___system_disk",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_system_uptime/config": `{
		"name": "__NAME__ Uptime",
		"icon": "mdi:clock-outline",
		"state_topic": "~/system/uptime",
		"unit_of_measurement": "s",
		"state_class": "total_increasing",
		"device_class": "duration",
		"avty_t": "~/available",
		"unique_id": "__VIN___system_uptime",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		// GPS tracking
		"%s/device_tracker/%s_gps/config": `{
		"name": "__NAME__ Location",
		"unique_id": "__VIN___gps_location",
		"icon": "mdi:map-marker",
		"~": "__TOPIC__",
		"json_attributes_topic": "~/gps/location",
		"avty_t": "~/available",
		"source_type": "gps",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		}
		}`,
		"%s/switch/%s_gps_enable/config": `{
		"name": "__NAME__ GPS Tracking",
		"icon": "mdi:map-marker",
		"state_topic": "~/gps/status",
		"command_topic": "~/set/gps",
		"payload_on": "on",
		"payload_off": "off",
		"state_on": "on",
		"state_off": "off",
		"avty_t": "~/available",
		"unique_id": "__VIN___gps_enable",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_gps_speed/config": `{
		"name": "__NAME__ Speed",
		"icon": "mdi:speedometer",
		"state_topic": "~/gps/speed",
		"unit_of_measurement": "km/h",
		"state_class": "measurement",
		"avty_t": "~/available",
		"unique_id": "__VIN___gps_speed",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_gps_altitude/config": `{
		"name": "__NAME__ Altitude",
		"icon": "mdi:elevation-rise",
		"state_topic": "~/gps/altitude",
		"unit_of_measurement": "m",
		"state_class": "measurement",
		"avty_t": "~/available",
		"unique_id": "__VIN___gps_altitude",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
		"%s/sensor/%s_gps_satellites/config": `{
		"name": "__NAME__ GPS Satellites",
		"icon": "mdi:satellite-variant",
		"state_topic": "~/gps/satellites",
		"state_class": "measurement",
		"avty_t": "~/available",
		"unique_id": "__VIN___gps_satellites",
		"dev": {
			"name": "PHEV __VIN__",
			"identifiers": ["phev-__VIN__"],
			"manufacturer": "Mitsubishi",
			"model": "Outlander PHEV"
		},
		"~": "__TOPIC__"}`,
	}
	mappings := map[string]string{
		"__NAME__":  name,
		"__VIN__":   vin,
		"__TOPIC__": topic,
	}
	for topic, d := range discoveryData {
		topic = fmt.Sprintf(topic, m.haDiscoveryPrefix, vin)
		for in, out := range mappings {
			d = strings.Replace(d, in, out, -1)
		}
		if token := m.client.Publish(topic, 0, true, d); token.Wait() && token.Error() != nil {
			log.Error(token.Error())
		}
		//m.client.Publish(topic, 0, false, "{}")
	}
}

func init() {
	clientCmd.AddCommand(mqttCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// mqttCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// mqttCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	mqttCmd.Flags().String("mqtt_server", "tcp://127.0.0.1:1883", "Address of MQTT server")
	mqttCmd.Flags().String("mqtt_username", "", "Username to login to MQTT server")
	mqttCmd.Flags().String("mqtt_password", "", "Password to login to MQTT server")
	mqttCmd.Flags().String("mqtt_topic_prefix", "phev", "Prefix for MQTT topics")
	mqttCmd.Flags().Bool("mqtt_disable_register_set_command", false, "Disable vechicle register setting via MQTT")
	mqttCmd.Flags().Bool("ha_discovery", true, "Enable Home Assistant MQTT discovery")
	mqttCmd.Flags().String("ha_discovery_prefix", "homeassistant", "Prefix for Home Assistant MQTT discovery")
	mqttCmd.Flags().Duration("update_interval", 5*time.Minute, "How often to request force updates")
	mqttCmd.Flags().Duration("wifi_restart_time", 0, "Attempt to restart Wifi if no connection for this long")
	mqttCmd.Flags().Duration("wifi_restart_retry_time", 2*time.Minute, "Interval to attempt Wifi restart")
	mqttCmd.Flags().String("wifi_restart_command", defaultWifiRestartCmd, "Command to restart Wifi connection to Phev")

	viper.BindPFlag("mqtt_server", mqttCmd.Flags().Lookup("mqtt_server"))
	viper.BindPFlag("mqtt_username", mqttCmd.Flags().Lookup("mqtt_username"))
	viper.BindPFlag("mqtt_password", mqttCmd.Flags().Lookup("mqtt_password"))
	viper.BindPFlag("mqtt_topic_prefix", mqttCmd.Flags().Lookup("mqtt_topic_prefix"))
	viper.BindPFlag("mqtt_disable_register_set_command", mqttCmd.Flags().Lookup("mqtt_disable_register_set_command"))
	viper.BindPFlag("ha_discovery", mqttCmd.Flags().Lookup("ha_discovery"))
	viper.BindPFlag("ha_discovery_prefix", mqttCmd.Flags().Lookup("ha_discovery_prefix"))
	viper.BindPFlag("update_interval", mqttCmd.Flags().Lookup("update_interval"))
	viper.BindPFlag("wifi_restart_time", mqttCmd.Flags().Lookup("wifi_restart_time"))
	viper.BindPFlag("wifi_restart_retry_time", mqttCmd.Flags().Lookup("wifi_restart_retry_time"))
	viper.BindPFlag("wifi_restart_command", mqttCmd.Flags().Lookup("wifi_restart_command"))
}
