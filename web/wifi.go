package web

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// WifiNetwork is a single scanned access point.
type WifiNetwork struct {
	SSID     string `json:"ssid"`
	BSSID    string `json:"bssid"`
	Signal   int    `json:"signal"` // 0..100
	Security string `json:"security"`
	InUse    bool   `json:"in_use"`
}

// WifiSaved is an nmcli connection profile for a wireless network.
type WifiSaved struct {
	Name        string `json:"name"`
	SSID        string `json:"ssid"`
	Autoconnect bool   `json:"autoconnect"`
	Active      bool   `json:"active"`
}

// WifiStatus is the current state of a given WiFi interface.
type WifiStatus struct {
	Interface string `json:"interface"`
	SSID      string `json:"ssid"`
	IPAddress string `json:"ip_address"`
	State     string `json:"state"`
	Available bool   `json:"available"` // false if nmcli missing
}

// WifiManager wraps nmcli for a single interface. Constructed by Server from
// the configured wifi_interface + wifi_use_sudo settings.
type WifiManager struct {
	Iface   string
	UseSudo bool
}

func (m *WifiManager) cmd(ctx context.Context, args ...string) *exec.Cmd {
	all := append([]string{"nmcli"}, args...)
	if m.UseSudo {
		all = append([]string{"sudo", "-n"}, all...)
	}
	return exec.CommandContext(ctx, all[0], all[1:]...)
}

// runText runs nmcli with the given args and returns combined stdout.
// Stderr is folded into the error on failure. Passwords are redacted so
// they don't leak into logs or HTTP responses.
func (m *WifiManager) runText(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := m.cmd(ctx, args...)
	out, err := c.Output()
	if err != nil {
		safe := redactPassword(args)
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("nmcli %s: %s: %s", strings.Join(safe, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return string(out), fmt.Errorf("nmcli %s: %w", strings.Join(safe, " "), err)
	}
	return string(out), nil
}

// redactPassword returns a copy of args with the value following the
// literal "password" arg replaced by "***" — keeps real secrets out of
// log lines and HTTP error responses.
func redactPassword(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out)-1; i++ {
		if out[i] == "password" {
			out[i+1] = "***"
		}
	}
	return out
}

// parseTerse splits an nmcli -t line on unescaped ':' separators. nmcli
// escapes literal ':' and '\\' with a backslash; undo that.
func parseTerse(line string) []string {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			cur.WriteByte(line[i+1])
			i++
			continue
		}
		if c == ':' {
			fields = append(fields, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	fields = append(fields, cur.String())
	return fields
}

// Scan returns nearby wireless networks visible to the configured interface.
// A rescan is requested (`--rescan auto`) so this may take a few seconds.
func (m *WifiManager) Scan() ([]WifiNetwork, error) {
	out, err := m.runText(15*time.Second,
		"-t", "-f", "IN-USE,BSSID,SSID,SIGNAL,SECURITY",
		"device", "wifi", "list", "ifname", m.Iface, "--rescan", "auto")
	if err != nil {
		return nil, err
	}
	var nets []WifiNetwork
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		f := parseTerse(line)
		if len(f) < 5 {
			continue
		}
		sig, _ := strconv.Atoi(f[3])
		nets = append(nets, WifiNetwork{
			InUse:    strings.TrimSpace(f[0]) == "*",
			BSSID:    f[1],
			SSID:     f[2],
			Signal:   sig,
			Security: f[4],
		})
	}
	return nets, nil
}

// Status returns the current state of the configured interface.
func (m *WifiManager) Status() (WifiStatus, error) {
	st := WifiStatus{Interface: m.Iface, Available: true}
	out, err := m.runText(5*time.Second,
		"-t", "-f", "DEVICE,STATE,CONNECTION", "device", "status")
	if err != nil {
		return st, err
	}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		f := parseTerse(scanner.Text())
		if len(f) < 3 || f[0] != m.Iface {
			continue
		}
		st.State = f[1]
		st.SSID = f[2]
		if st.SSID == "--" {
			st.SSID = ""
		}
		break
	}
	// IP address (first IPv4)
	ipOut, err := m.runText(5*time.Second,
		"-t", "-f", "IP4.ADDRESS", "device", "show", m.Iface)
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(ipOut))
		for scanner.Scan() {
			line := scanner.Text()
			// "IP4.ADDRESS[1]:192.168.8.42/24"
			if idx := strings.Index(line, ":"); idx > 0 {
				addr := line[idx+1:]
				if slash := strings.Index(addr, "/"); slash > 0 {
					addr = addr[:slash]
				}
				st.IPAddress = addr
				break
			}
		}
	}
	return st, nil
}

// Connect joins the given SSID. Pass empty password for open networks.
// hidden=true asks NetworkManager to probe for non-broadcast SSIDs.
func (m *WifiManager) Connect(ssid, password string, hidden bool) error {
	if ssid == "" {
		return fmt.Errorf("ssid is required")
	}
	args := []string{"device", "wifi", "connect", ssid}
	if password != "" {
		args = append(args, "password", password)
	}
	args = append(args, "ifname", m.Iface)
	if hidden {
		args = append(args, "hidden", "yes")
	}
	// nmcli waits for the connection to come up by default; give it a
	// generous timeout.
	_, err := m.runText(45*time.Second, args...)
	return err
}

// ListSaved returns wireless connection profiles known to NetworkManager.
// Includes all interfaces (not filtered by m.Iface) because saved profiles
// aren't always bound to a specific device.
func (m *WifiManager) ListSaved() ([]WifiSaved, error) {
	out, err := m.runText(5*time.Second,
		"-t", "-f", "NAME,TYPE,DEVICE,AUTOCONNECT,ACTIVE", "connection", "show")
	if err != nil {
		return nil, err
	}
	var saved []WifiSaved
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		f := parseTerse(scanner.Text())
		if len(f) < 5 || f[1] != "802-11-wireless" {
			continue
		}
		saved = append(saved, WifiSaved{
			Name:        f[0],
			SSID:        f[0], // name defaults to ssid in nmcli; close enough for UI
			Autoconnect: f[3] == "yes",
			Active:      f[4] == "yes",
		})
	}
	return saved, nil
}

// Forget deletes a saved nmcli connection profile by name.
func (m *WifiManager) Forget(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	_, err := m.runText(5*time.Second, "connection", "delete", name)
	return err
}
