/*
Copyright © 2026 Ben Buxton <bbuxton@gmail.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
package cmd

import (
	"fmt"
	"time"

	"github.com/buxtronix/phev2mqtt/client"
	"github.com/buxtronix/phev2mqtt/protocol"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show vehicle status",
	Long: `Display current vehicle status including battery, charging, doors, lights, and climate.

Example:
  phev2mqtt client status`,
	Run: runStatus,
}

type vehicleStatus struct {
	VIN              string
	BatteryLevel     int
	Charging         bool
	ChargeRemaining  int
	ChargerConnected bool
	DoorsLocked      bool
	ParkingLights    bool
	Headlights       bool
	ClimateMode      string
	ClimateState     string
}

func runStatus(cmd *cobra.Command, args []string) {
	// Connect to car
	address, _ := cmd.Flags().GetString("address")
	cl, err := client.New(client.AddressOption(address))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	if err := cl.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	if err := cl.Start(); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	log.Infof("Connected to car, reading status...")

	status := &vehicleStatus{}
	timeout := time.After(15 * time.Second)
	receivedData := make(map[byte]bool)

	// Collect status from various registers
	for {
		select {
		case <-timeout:
			goto DISPLAY
		case msg := <-cl.Recv:
			if msg.Reg == nil {
				continue
			}

			receivedData[msg.Register] = true

			switch reg := msg.Reg.(type) {
			case *protocol.RegisterVIN:
				status.VIN = reg.VIN
			case *protocol.RegisterBatteryLevel:
				status.BatteryLevel = reg.Level
				status.ParkingLights = reg.ParkingLights
			case *protocol.RegisterChargeStatus:
				status.Charging = reg.Charging
				status.ChargeRemaining = reg.Remaining
			case *protocol.RegisterChargePlug:
				status.ChargerConnected = reg.Connected
			case *protocol.RegisterDoorStatus:
				status.DoorsLocked = reg.Locked
				status.Headlights = reg.Headlights
			case *protocol.RegisterACMode:
				status.ClimateMode = reg.Mode
			case *protocol.RegisterPreACState:
				switch reg.State {
				case protocol.PreACOff:
					status.ClimateState = "off"
				case protocol.PreACOn:
					status.ClimateState = "on"
				case protocol.PreACTerminated:
					status.ClimateState = "terminated"
				default:
					status.ClimateState = "unknown"
				}
			}

			// Check if we have enough data
			if len(receivedData) >= 6 {
				goto DISPLAY
			}
		}
	}

DISPLAY:
	displayStatus(status)
	cl.Close()
}

func displayStatus(s *vehicleStatus) {
	fmt.Println("\n╔════════════════════════════════════════════════╗")
	fmt.Println("║         MITSUBISHI OUTLANDER PHEV STATUS       ║")
	fmt.Println("╠════════════════════════════════════════════════╣")

	// Vehicle Info
	if s.VIN != "" {
		fmt.Printf("║ VIN: %-42s ║\n", s.VIN)
		fmt.Println("╠════════════════════════════════════════════════╣")
	}

	// Battery & Charging
	fmt.Println("║ BATTERY & CHARGING                             ║")
	fmt.Println("╟────────────────────────────────────────────────╢")
	fmt.Printf("║ Battery Level:      %3d%%                        ║\n", s.BatteryLevel)

	chargerStatus := "Disconnected"
	if s.ChargerConnected {
		chargerStatus = "Connected"
	}
	fmt.Printf("║ Charger:            %-23s ║\n", chargerStatus)

	chargingStatus := "Not Charging"
	if s.Charging {
		chargingStatus = fmt.Sprintf("Charging (%d min remaining)", s.ChargeRemaining)
	}
	fmt.Printf("║ Status:             %-23s ║\n", chargingStatus)

	// Doors & Locks
	fmt.Println("╠════════════════════════════════════════════════╣")
	fmt.Println("║ DOORS & SECURITY                               ║")
	fmt.Println("╟────────────────────────────────────────────────╢")

	lockStatus := "Unlocked 🔓"
	if s.DoorsLocked {
		lockStatus = "Locked 🔒"
	}
	fmt.Printf("║ Doors:              %-23s ║\n", lockStatus)

	// Lights
	fmt.Println("╠════════════════════════════════════════════════╣")
	fmt.Println("║ LIGHTS                                         ║")
	fmt.Println("╟────────────────────────────────────────────────╢")

	parkingStatus := "Off"
	if s.ParkingLights {
		parkingStatus = "On 💡"
	}
	fmt.Printf("║ Parking Lights:     %-23s ║\n", parkingStatus)

	headlightStatus := "Off"
	if s.Headlights {
		headlightStatus = "On 💡"
	}
	fmt.Printf("║ Headlights:         %-23s ║\n", headlightStatus)

	// Climate
	fmt.Println("╠════════════════════════════════════════════════╣")
	fmt.Println("║ CLIMATE CONTROL                                ║")
	fmt.Println("╟────────────────────────────────────────────────╢")

	climateIcon := ""
	switch s.ClimateMode {
	case "heat":
		climateIcon = "🔥"
	case "cool":
		climateIcon = "❄️"
	case "windscreen":
		climateIcon = "💨"
	}

	if s.ClimateState == "on" && s.ClimateMode != "" && s.ClimateMode != "unknown" {
		fmt.Printf("║ Status:             Active %s                    ║\n", climateIcon)
		fmt.Printf("║ Mode:               %-23s ║\n", s.ClimateMode)
	} else if s.ClimateState == "terminated" {
		fmt.Printf("║ Status:             %-23s ║\n", "Terminated (reset needed)")
	} else {
		fmt.Printf("║ Status:             %-23s ║\n", "Inactive")
	}

	fmt.Println("╚════════════════════════════════════════════════╝\n")
}

func init() {
	clientCmd.AddCommand(statusCmd)
}
