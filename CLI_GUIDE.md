# PHEV2MQTT Complete CLI Guide

Comprehensive command-line interface for all Mitsubishi Outlander PHEV controls.

## Table of Contents
- [Quick Reference](#quick-reference)
- [Climate Control](#climate-control)
- [Lights Control](#lights-control)
- [Charging Control](#charging-control)
- [Vehicle Status](#vehicle-status)
- [Registration](#registration)
- [Advanced](#advanced)

---

## Quick Reference

### Most Common Commands

```bash
# Climate Control
phev2mqtt client climate start heat 20        # Start heat for 20 mins
phev2mqtt client climate stop                 # Stop climate
phev2mqtt client climate timer set 1 7 30 heat 20 mon tue wed thu fri

# Lights
phev2mqtt client lights parking on            # Parking lights on
phev2mqtt client lights headlights off        # Headlights off

# Status
phev2mqtt client status                       # Show all vehicle status

# Charging
phev2mqtt client charging cancel-timer        # Cancel charge timer
```

---

## Climate Control

### Immediate Control

#### Start Climate Now
```bash
phev2mqtt client climate start [mode] [duration]
```

**Modes:** `heat`, `cool`, `windscreen`
**Duration:** `10`, `20`, `30` (minutes)

**Examples:**
```bash
# Heat for 20 minutes
phev2mqtt client climate start heat 20

# Cool for 10 minutes
phev2mqtt client climate start cool 10

# Defrost windscreen for 30 minutes
phev2mqtt client climate start windscreen 30
```

#### Stop Climate
```bash
phev2mqtt client climate stop
```

### Timer Management

#### Set a Timer
```bash
phev2mqtt client climate timer set [id] [hour] [minute] [mode] [duration] [days...]
```

**Parameters:**
- `id`: 1-5
- `hour`: 0-23
- `minute`: 0, 10, 20, 30, 40, 50
- `mode`: heat, cool, windscreen
- `duration`: 10, 20, 30 (minutes)
- `days`: sun, mon, tue, wed, thu, fri, sat

**Examples:**
```bash
# Weekday morning heat at 7:30 AM
phev2mqtt client climate timer set 1 7 30 heat 20 mon tue wed thu fri

# Weekend cool at 6:00 PM
phev2mqtt client climate timer set 2 18 0 cool 10 sat sun

# Daily windscreen defrost at 6:00 AM
phev2mqtt client climate timer set 3 6 0 windscreen 30 sun mon tue wed thu fri sat
```

#### Clear Timers
```bash
# Clear specific timer
phev2mqtt client climate timer clear 1

# Clear all timers
phev2mqtt client climate timer clear all
```

#### List Timers
```bash
phev2mqtt client climate timer list
```

**Output:**
```
═══════════════════════════════════════
     PHEV Climate Timers
═══════════════════════════════════════

Timer 1:
  {
    "enabled": true,
    "hour": 7,
    "minute": 30,
    "mode": "heat",
    "duration": 20,
    "days": ["mon","tue","wed","thu","fri"]
  }
Timer 2: DISABLED
...
```

---

## Lights Control

### Parking Lights
```bash
# Turn on
phev2mqtt client lights parking on

# Turn off
phev2mqtt client lights parking off
```

### Headlights
```bash
# Turn on
phev2mqtt client lights headlights on

# Turn off
phev2mqtt client lights headlights off
```

---

## Charging Control

### Cancel Charge Timer
```bash
phev2mqtt client charging cancel-timer
```

Cancels the scheduled charge timer, allowing immediate charging when plugged in.

---

## Vehicle Status

### Display Complete Status
```bash
phev2mqtt client status
```

**Output:**
```
╔════════════════════════════════════════════════╗
║         MITSUBISHI OUTLANDER PHEV STATUS       ║
╠════════════════════════════════════════════════╣
║ VIN: JMFXDGG2WJZ00048                          ║
╠════════════════════════════════════════════════╣
║ BATTERY & CHARGING                             ║
╟────────────────────────────────────────────────╢
║ Battery Level:       85%                       ║
║ Charger:            Connected                  ║
║ Status:             Charging (45 min remaining)║
╠════════════════════════════════════════════════╣
║ DOORS & SECURITY                               ║
╟────────────────────────────────────────────────╢
║ Doors:              Locked 🔒                   ║
╠════════════════════════════════════════════════╣
║ LIGHTS                                         ║
╟────────────────────────────────────────────────╢
║ Parking Lights:     Off                        ║
║ Headlights:         Off                        ║
╠════════════════════════════════════════════════╣
║ CLIMATE CONTROL                                ║
╟────────────────────────────────────────────────╢
║ Status:             Active 🔥                   ║
║ Mode:               heat                       ║
╚════════════════════════════════════════════════╝
```

---

## Registration

### Register Client
```bash
phev2mqtt client register
```

**Requirements:**
1. Car must be in registration mode (button sequence in car)
2. Run command within 5 minutes
3. Car will remember the device MAC address

### Unregister Client
```bash
phev2mqtt client unregister
```

Removes the current device from the car's authorized client list.

---

## Advanced

### Watch Mode
```bash
phev2mqtt client watch
```

Connect to the car and display all incoming register updates in real-time.
Useful for debugging and monitoring.

### Direct Register Control
```bash
# Set a register directly
phev2mqtt client set 0b:02

# Set multiple registers
phev2mqtt client set 0b:02 0a:01

# With custom timing
phev2mqtt client set 0b:02 --wait_duration 5s --send_interval 2s
```

**Common Registers:**
- `0x0a`: Headlights (01=on, 02=off)
- `0x0b`: Parking lights (01=on, 02=off)
- `0x13`: Reset climate state
- `0x17`: Charge timer control

---

## Global Flags

All commands support these flags:

### Connection
```bash
--address string    # Car address (default: "192.168.8.46:8080")
```

**Example:**
```bash
phev2mqtt client status --address 192.168.1.100:8080
```

### Logging
```bash
-v, --verbosity string    # Logging level: debug, info, warn, error
-t, --log_timestamps      # Show timestamps in logs
-s, --log_syslog          # Log to syslog instead of console
```

**Examples:**
```bash
# Debug mode
phev2mqtt client climate start heat 20 -v debug

# With timestamps
phev2mqtt client status -t

# Both
phev2mqtt client climate timer list -v debug -t
```

### Configuration File
```bash
--config string    # Config file path (default: $HOME/.phev2mqtt.yaml)
```

---

## MQTT Equivalent Commands

For reference, here are the MQTT equivalents:

| CLI Command | MQTT Topic | Payload |
|-------------|------------|---------|
| `climate start heat 20` | `phev/set/climate/now` | `heat:20` |
| `climate stop` | `phev/set/climate/stop` | ` ` |
| `lights parking on` | `phev/set/parkinglights` | `on` |
| `lights headlights off` | `phev/set/headlights` | `off` |
| `charging cancel-timer` | `phev/set/cancelchargetimer` | ` ` |

---

## Common Use Cases

### 1. Morning Routine
```bash
# Set up weekday morning heat
phev2mqtt client climate timer set 1 7 30 heat 20 mon tue wed thu fri

# Verify it's set
phev2mqtt client climate timer list
```

### 2. Remote Start Before Leaving
```bash
# Check current status
phev2mqtt client status

# Start heating
phev2mqtt client climate start heat 20
```

### 3. Winter Morning Setup
```bash
# Weekday: Heat cabin + defrost windows
phev2mqtt client climate timer set 1 6 30 heat 30 mon tue wed thu fri
phev2mqtt client climate timer set 2 6 50 windscreen 20 mon tue wed thu fri
```

### 4. Emergency Stop Everything
```bash
# Stop climate
phev2mqtt client climate stop

# Turn off all lights
phev2mqtt client lights parking off
phev2mqtt client lights headlights off
```

### 5. Summer Weekend Cool Down
```bash
# Before shopping trip on Saturday
phev2mqtt client climate timer set 3 14 30 cool 10 sat
```

---

## Troubleshooting

### Connection Failed
```bash
# Check connectivity
ping 192.168.8.46

# Try with debug logging
phev2mqtt client status -v debug

# Check if car WiFi is on and connected
```

### Command Timeout
For MY2014/2015 models, the enhanced timeout is automatic:
- 20-second timeout (vs 10 for newer models)
- Automatic retry on errors
- Up to 3 attempts per command

### Timer Not Activating
```bash
# Verify timer is set correctly
phev2mqtt client climate timer list

# Check car's clock is correct
# Timers use the car's internal clock, not phone/system time

# Clear and reset if needed
phev2mqtt client climate timer clear all
phev2mqtt client climate timer set 1 [your settings]
```

### Register Command Not Working
```bash
# Must be in registration mode first
# 1. Get in car and press registration button sequence
# 2. Run command within 5 minutes
phev2mqtt client register
```

---

## Model Year Differences

### MY2014/2015 (2014-2015 Outlander PHEV)
- Enhanced timeout handling (20s)
- Uses different climate control registers
- Automatic protocol detection
- All commands work the same

### MY2018+ (2018+ Outlander PHEV)
- Standard timeout (10s)
- Different internal registers
- All commands work the same

**Note:** You don't need to specify model year - it's detected automatically!

---

## Tips & Tricks

### 1. Create Shell Aliases
```bash
# Add to ~/.bashrc or ~/.zshrc
alias phev-heat='phev2mqtt client climate start heat 20'
alias phev-cool='phev2mqtt client climate start cool 10'
alias phev-stop='phev2mqtt client climate stop'
alias phev-status='phev2mqtt client status'

# Then use:
phev-heat
phev-status
```

### 2. Script Complex Operations
```bash
#!/bin/bash
# morning-routine.sh

echo "Starting morning routine..."
phev2mqtt client lights parking on
sleep 2
phev2mqtt client climate start heat 20
echo "Done! Car will be warm in 20 minutes."
```

### 3. Combine with cron
```bash
# Edit crontab
crontab -e

# Heat car weekdays at 7:30 AM (uses timer, more reliable)
0 7 * * 1-5 /usr/local/bin/phev2mqtt client climate start heat 20
```

### 4. Use with Home Assistant
See `CLIMATE_CLI.md` for Home Assistant integration examples.

---

## Complete Command Tree

```
phev2mqtt client
├── climate
│   ├── start [mode] [duration]
│   ├── stop
│   └── timer
│       ├── set [id] [hour] [minute] [mode] [duration] [days...]
│       ├── clear [id|all]
│       └── list
├── lights
│   ├── parking [on|off]
│   └── headlights [on|off]
├── charging
│   └── cancel-timer
├── status
├── register
├── unregister
├── watch
└── set [register:value...]
```

---

## Getting Help

### Command-Specific Help
```bash
# Top level
phev2mqtt --help

# Client commands
phev2mqtt client --help

# Specific command
phev2mqtt client climate --help
phev2mqtt client climate start --help
phev2mqtt client climate timer set --help
```

### Online Resources
- Protocol documentation: `protocol/README.md`
- Climate CLI guide: `CLIMATE_CLI.md`
- GitHub issues: https://github.com/buxtronix/phev2mqtt/issues

---

## Version
This guide is for phev2mqtt with:
- ✅ Complete climate control (immediate + timers)
- ✅ Lights control (parking + headlights)
- ✅ Charging control
- ✅ Full status display
- ✅ Enhanced MY2014/2015 support

Built with ❤️ for PHEV owners who love the command line!
