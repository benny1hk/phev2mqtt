# PHEV Climate Control CLI Guide

Complete command-line interface for controlling PHEV climate system.

## Table of Contents
- [Quick Start](#quick-start)
- [Immediate Climate Control](#immediate-climate-control)
- [Climate Timer Management](#climate-timer-management)
- [MQTT Topics Reference](#mqtt-topics-reference)
- [Examples](#examples)

---

## Quick Start

### Start Climate Control Now
```bash
# Heat for 20 minutes
phev2mqtt client climate start heat 20

# Cool for 10 minutes
phev2mqtt client climate start cool 10

# Windscreen defrost for 30 minutes
phev2mqtt client climate start windscreen 30
```

### Stop Climate Control
```bash
phev2mqtt client climate stop
```

---

## Immediate Climate Control

### `phev2mqtt client climate start [mode] [duration]`

Start climate control immediately.

**Parameters:**
- `mode`: `heat`, `cool`, or `windscreen`
- `duration`: `10`, `20`, or `30` (minutes)

**Examples:**
```bash
# Start heating for 20 minutes
phev2mqtt client climate start heat 20

# Start cooling for 10 minutes
phev2mqtt client climate start cool 10

# Start windscreen defrost for 30 minutes
phev2mqtt client climate start windscreen 30
```

**MQTT Equivalent:**
```bash
# Using simple format
mosquitto_pub -t "phev/set/climate/now" -m "heat:20"

# Using existing switches
mosquitto_pub -t "phev/set/climate/heat" -m "20"
```

### `phev2mqtt client climate stop`

Stop any running climate control immediately.

**Example:**
```bash
phev2mqtt client climate stop
```

**MQTT Equivalent:**
```bash
mosquitto_pub -t "phev/set/climate/stop" -m ""
```

---

## Climate Timer Management

### `phev2mqtt client climate timer set [id] [hour] [minute] [mode] [duration] [days...]`

Set a scheduled climate timer (1-5).

**Parameters:**
- `id`: Timer slot number `1` to `5`
- `hour`: Hour `0` to `23`
- `minute`: Minute `0`, `10`, `20`, `30`, `40`, or `50`
- `mode`: `heat`, `cool`, or `windscreen`
- `duration`: `10`, `20`, or `30` (minutes)
- `days`: Any combination of `sun`, `mon`, `tue`, `wed`, `thu`, `fri`, `sat`

**Examples:**
```bash
# Weekday morning heat - Timer 1: 7:30 AM heat for 20 mins Mon-Fri
phev2mqtt client climate timer set 1 7 30 heat 20 mon tue wed thu fri

# Weekend cool - Timer 2: 6:00 PM cool for 10 mins Sat-Sun
phev2mqtt client climate timer set 2 18 0 cool 10 sat sun

# Daily windscreen - Timer 3: 6:00 AM windscreen for 30 mins every day
phev2mqtt client climate timer set 3 6 0 windscreen 30 sun mon tue wed thu fri sat

# Single day - Timer 4: Saturday morning heat
phev2mqtt client climate timer set 4 8 0 heat 20 sat
```

**MQTT Equivalent:**
```bash
# Set timer 1
mosquitto_pub -t "phev/set/climate/timer/1" -m '{
  "enabled": true,
  "hour": 7,
  "minute": 30,
  "mode": "heat",
  "duration": 20,
  "days": ["mon","tue","wed","thu","fri"]
}'
```

### `phev2mqtt client climate timer clear [id|all]`

Clear one or all climate timers.

**Parameters:**
- `id`: Timer number `1` to `5`, or `all` for all timers

**Examples:**
```bash
# Clear timer 1
phev2mqtt client climate timer clear 1

# Clear all timers
phev2mqtt client climate timer clear all
```

**MQTT Equivalent:**
```bash
# Clear all timers
mosquitto_pub -t "phev/set/climate/timer/clear" -m ""

# Disable specific timer (set enabled to false)
mosquitto_pub -t "phev/set/climate/timer/1" -m '{"enabled": false}'
```

### `phev2mqtt client climate timer list`

Display all configured climate timers.

**Example:**
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
Timer 3: DISABLED
Timer 4: DISABLED
Timer 5: DISABLED

═══════════════════════════════════════
```

---

## MQTT Topics Reference

### Immediate Control

| Topic | Payload | Description |
|-------|---------|-------------|
| `phev/set/climate/heat` | `on`, `10`, `20`, `30`, `off` | Heat control |
| `phev/set/climate/cool` | `on`, `10`, `20`, `30`, `off` | Cool control |
| `phev/set/climate/windscreen` | `on`, `10`, `20`, `30`, `off` | Windscreen defrost |
| `phev/set/climate/mode` | `heat`, `cool`, `windscreen`, `off` | Mode selector |
| `phev/set/climate/now` | `heat:20`, `cool:10`, `windscreen:30`, `off` | Simple format |
| `phev/set/climate/stop` | (any) | Stop climate |
| `phev/set/climate/start` | JSON | JSON format start |

### Timer Management

| Topic | Payload | Description |
|-------|---------|-------------|
| `phev/set/climate/timer/1` | JSON | Set timer 1 |
| `phev/set/climate/timer/2` | JSON | Set timer 2 |
| `phev/set/climate/timer/3` | JSON | Set timer 3 |
| `phev/set/climate/timer/4` | JSON | Set timer 4 |
| `phev/set/climate/timer/5` | JSON | Set timer 5 |
| `phev/set/climate/timer/clear` | (any) | Clear all timers |

### Status Topics (Published by Car)

| Topic | Format | Description |
|-------|--------|-------------|
| `phev/climate/mode` | `heat`, `cool`, `windscreen`, `off` | Current mode |
| `phev/climate/state` | `heat`, `cool`, `windscreen`, `off`, `terminated` | Current state |
| `phev/climate/timer/1` | JSON | Timer 1 status |
| `phev/climate/timer/2` | JSON | Timer 2 status |
| `phev/climate/timer/3` | JSON | Timer 3 status |
| `phev/climate/timer/4` | JSON | Timer 4 status |
| `phev/climate/timer/5` | JSON | Timer 5 status |

---

## Examples

### Common Use Cases

#### 1. Morning Weekday Routine
```bash
# Set timer to heat car every weekday morning at 7:30 AM for 20 minutes
phev2mqtt client climate timer set 1 7 30 heat 20 mon tue wed thu fri
```

#### 2. Remote Start Before Leaving
```bash
# Start heating immediately for 20 minutes before heading to car
phev2mqtt client climate start heat 20
```

#### 3. Summer Weekend Cooling
```bash
# Set timer to cool car on weekend afternoons at 5:00 PM for 10 minutes
phev2mqtt client climate timer set 2 17 0 cool 10 sat sun
```

#### 4. Winter Morning Defrost
```bash
# Set timer to defrost windscreen every morning at 6:00 AM for 30 minutes
phev2mqtt client climate timer set 3 6 0 windscreen 30 sun mon tue wed thu fri sat
```

#### 5. Emergency Stop
```bash
# Stop climate control if you realize you left it running
phev2mqtt client climate stop
```

#### 6. Clear All Schedules
```bash
# Remove all timer schedules (e.g., when going on vacation)
phev2mqtt client climate timer clear all
```

### Home Assistant Integration

#### Script for Quick Heat
```yaml
script:
  phev_heat_now:
    alias: "PHEV Heat Now"
    sequence:
      - service: mqtt.publish
        data:
          topic: phev/set/climate/now
          payload: "heat:20"
```

#### Automation for Work Days
```yaml
automation:
  - alias: "PHEV Weekday Morning Heat"
    trigger:
      - platform: time
        at: "07:30:00"
    condition:
      - condition: time
        weekday:
          - mon
          - tue
          - wed
          - thu
          - fri
    action:
      - service: mqtt.publish
        data:
          topic: phev/set/climate/start
          payload: '{"mode":"heat","duration":20}'
```

---

## Connection Options

All commands support the `--address` flag to specify the car's IP:

```bash
# Default address (192.168.8.46:8080)
phev2mqtt client climate start heat 20

# Custom address
phev2mqtt client climate start heat 20 --address 192.168.1.100:8080
```

## Logging Options

Control verbosity with global flags:

```bash
# Debug mode
phev2mqtt client climate start heat 20 -v debug

# Timestamps
phev2mqtt client climate start heat 20 -t

# Syslog
phev2mqtt client climate start heat 20 -s
```

---

## Troubleshooting

### Connection Failed
```bash
# Check car WiFi is on and connected
# Verify address with:
ping 192.168.8.46
```

### Timer Not Working
```bash
# Verify timer is set:
phev2mqtt client climate timer list

# Check car clock is correct
# Timers use car's internal clock
```

### MY2014/2015 Timeout Issues
The enhanced timeout handling for MY2014/2015 models is built-in:
- 20-second timeout (vs 10 seconds for other models)
- Automatic retry on XOR encoding errors
- Up to 3 attempts per command

---

## Protocol Notes

### MY2014/2015 (2014-2015 Outlander PHEV)
- Uses register `0x02` for climate mode setting
- Uses register `0x04` for enable/disable
- Uses register `0x05` for timer-based activation
- Enhanced timeout handling (20 seconds)

### MY2018+ (2018+ Outlander PHEV)
- Uses register `0x1b` for immediate climate control
- Standard timeout handling (10 seconds)

### All Models
- Register `0x1a` for setting timer schedules
- Register `0x05` for reading timer status
- Register `0x13` for stop/reset
- Register `0x1c` for reading current mode

---

## Version
This documentation is for phev2mqtt with enhanced MY2014 support and comprehensive climate control features.

Built with ❤️ for Mitsubishi Outlander PHEV owners.
