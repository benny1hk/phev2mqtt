# MQTT to CLI Command Mapping

Complete mapping between MQTT topics and CLI commands for all PHEV controls.

## Climate Control

| MQTT Topic | MQTT Payload | CLI Command |
|------------|--------------|-------------|
| `phev/set/climate/heat` | `10`, `20`, `30` | `phev2mqtt client climate start heat [10\|20\|30]` |
| `phev/set/climate/cool` | `10`, `20`, `30` | `phev2mqtt client climate start cool [10\|20\|30]` |
| `phev/set/climate/windscreen` | `10`, `20`, `30` | `phev2mqtt client climate start windscreen [10\|20\|30]` |
| `phev/set/climate/mode` | `heat`, `cool`, `windscreen`, `off` | `phev2mqtt client climate start [mode] [duration]` |
| `phev/set/climate/now` | `heat:20`, `cool:10`, etc. | `phev2mqtt client climate start [mode] [duration]` |
| `phev/set/climate/start` | JSON | `phev2mqtt client climate start [mode] [duration]` |
| `phev/set/climate/stop` | (any) | `phev2mqtt client climate stop` |
| `phev/set/climate/timer/1` | JSON | `phev2mqtt client climate timer set 1 [hour] [min] [mode] [dur] [days...]` |
| `phev/set/climate/timer/2` | JSON | `phev2mqtt client climate timer set 2 [hour] [min] [mode] [dur] [days...]` |
| `phev/set/climate/timer/3` | JSON | `phev2mqtt client climate timer set 3 [hour] [min] [mode] [dur] [days...]` |
| `phev/set/climate/timer/4` | JSON | `phev2mqtt client climate timer set 4 [hour] [min] [mode] [dur] [days...]` |
| `phev/set/climate/timer/5` | JSON | `phev2mqtt client climate timer set 5 [hour] [min] [mode] [dur] [days...]` |
| `phev/set/climate/timer/clear` | (any) | `phev2mqtt client climate timer clear all` |

## Lights Control

| MQTT Topic | MQTT Payload | CLI Command |
|------------|--------------|-------------|
| `phev/set/parkinglights` | `on`, `off` | `phev2mqtt client lights parking [on\|off]` |
| `phev/set/headlights` | `on`, `off` | `phev2mqtt client lights headlights [on\|off]` |

## Charging Control

| MQTT Topic | MQTT Payload | CLI Command |
|------------|--------------|-------------|
| `phev/set/cancelchargetimer` | (any) | `phev2mqtt client charging cancel-timer` |

## Direct Register Control

| MQTT Topic | MQTT Payload | CLI Command |
|------------|--------------|-------------|
| `phev/set/register/XX` | hex data | `phev2mqtt client set XX:YY [XX:YY...]` |

## Vehicle Status (Read-Only)

| MQTT Topics | CLI Command |
|-------------|-------------|
| `phev/battery/*`, `phev/door/*`, `phev/climate/*`, `phev/charge/*` | `phev2mqtt client status` |

## Examples

### Climate Control Examples

```bash
# MQTT
mosquitto_pub -t "phev/set/climate/heat" -m "20"
# CLI
phev2mqtt client climate start heat 20

# MQTT
mosquitto_pub -t "phev/set/climate/stop" -m ""
# CLI
phev2mqtt client climate stop

# MQTT
mosquitto_pub -t "phev/set/climate/timer/1" -m '{"enabled":true,"hour":7,"minute":30,"mode":"heat","duration":20,"days":["mon","tue","wed","thu","fri"]}'
# CLI
phev2mqtt client climate timer set 1 7 30 heat 20 mon tue wed thu fri
```

### Lights Examples

```bash
# MQTT
mosquitto_pub -t "phev/set/parkinglights" -m "on"
# CLI
phev2mqtt client lights parking on

# MQTT
mosquitto_pub -t "phev/set/headlights" -m "off"
# CLI
phev2mqtt client lights headlights off
```

### Charging Examples

```bash
# MQTT
mosquitto_pub -t "phev/set/cancelchargetimer" -m ""
# CLI
phev2mqtt client charging cancel-timer
```

### Status Examples

```bash
# View all vehicle status
phev2mqtt client status
```

## Notes

- All CLI commands support the `--address` flag to specify vehicle IP
- All CLI commands support logging flags: `-v`, `-t`, `-s`
- CLI commands provide immediate feedback and validation
- MQTT topics remain available for Home Assistant and other integrations
- The `/connection` MQTT topic controls the MQTT bridge itself (not a vehicle control)

## Complete Coverage

✅ **All vehicle control MQTT topics now have CLI equivalents**

Every writable MQTT topic (`phev/set/*`) that controls the vehicle has a corresponding CLI command with proper argument validation, help text, and examples.
