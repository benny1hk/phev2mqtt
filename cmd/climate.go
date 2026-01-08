/*
Copyright © 2026 Ben Buxton <bbuxton@gmail.com>

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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/buxtronix/phev2mqtt/client"
	"github.com/buxtronix/phev2mqtt/protocol"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// climateCmd represents the climate command
var climateCmd = &cobra.Command{
	Use:   "climate",
	Short: "Control climate (heating/cooling)",
	Long: `Control the PHEV climate control system.

Available subcommands:
  start      - Start climate control immediately
  stop       - Stop climate control
  timer      - Manage climate timers
  status     - Show current climate status`,
}

// climateStartCmd starts climate control immediately
var climateStartCmd = &cobra.Command{
	Use:   "start [mode] [duration]",
	Short: "Start climate control now",
	Long: `Start climate control immediately.

Modes: heat, cool, windscreen
Duration: 10, 20, or 30 (minutes)

Examples:
  phev2mqtt client climate start heat 20
  phev2mqtt client climate start cool 10
  phev2mqtt client climate start windscreen 30`,
	Args: cobra.ExactArgs(2),
	Run:  runClimateStart,
}

// climateStopCmd stops climate control
var climateStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop climate control",
	Long:  `Stop any running climate control immediately.`,
	Run:   runClimateStop,
}

// climateTimerCmd manages climate timers
var climateTimerCmd = &cobra.Command{
	Use:   "timer",
	Short: "Manage climate timers",
	Long: `Manage climate timer schedules.

Available subcommands:
  set     - Set a timer
  clear   - Clear timer(s)
  list    - List all timers`,
}

// climateTimerSetCmd sets a timer
var climateTimerSetCmd = &cobra.Command{
	Use:   "set [timer-id] [hour] [minute] [mode] [duration] [days...]",
	Short: "Set a climate timer",
	Long: `Set a climate timer schedule.

timer-id: 1-5
hour: 0-23
minute: 0, 10, 20, 30, 40, 50
mode: heat, cool, windscreen
duration: 10, 20, 30 (minutes)
days: sun, mon, tue, wed, thu, fri, sat (space-separated)

Examples:
  phev2mqtt client climate timer set 1 7 30 heat 20 mon tue wed thu fri
  phev2mqtt client climate timer set 2 18 0 cool 10 sat sun`,
	Args: cobra.MinimumNArgs(6),
	Run:  runClimateTimerSet,
}

// climateTimerClearCmd clears timer(s)
var climateTimerClearCmd = &cobra.Command{
	Use:   "clear [timer-id|all]",
	Short: "Clear climate timer(s)",
	Long: `Clear one or all climate timers.

Examples:
  phev2mqtt client climate timer clear 1
  phev2mqtt client climate timer clear all`,
	Args: cobra.ExactArgs(1),
	Run:  runClimateTimerClear,
}

// climateTimerListCmd lists all timers
var climateTimerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all climate timers",
	Long:  `Display all configured climate timers.`,
	Run:   runClimateTimerList,
}

func runClimateStart(cmd *cobra.Command, args []string) {
	mode := strings.ToLower(args[0])
	durationStr := args[1]

	// Validate mode
	validModes := map[string]byte{"cool": 0x1, "heat": 0x2, "windscreen": 0x3}
	modeCode, ok := validModes[mode]
	if !ok {
		log.Fatalf("Invalid mode: %s (must be cool, heat, or windscreen)", mode)
	}

	// Validate duration
	validDurations := map[string]byte{"10": 0x0, "20": 0x1, "30": 0x2}
	durationCode, ok := validDurations[durationStr]
	if !ok {
		log.Fatalf("Invalid duration: %s (must be 10, 20, or 30)", durationStr)
	}

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

	log.Infof("Connected to car, starting climate control...")
	time.Sleep(2 * time.Second)

	// Determine model year approach
	if cl.ModelYear == client.ModelYear14 {
		// MY2014 approach - use register 0x05
		timerPayload := make([]byte, 16)
		timerPayload[0] = 0x01

		// Create immediate timer
		now := time.Now()
		immediateTimer := protocol.ClimateTimer{
			Enabled:  true,
			Hour:     uint8(now.Hour()),
			Minute:   uint8((now.Minute() / 10) * 10),
			Mode:     mode,
			Duration: uint8((durationCode + 1) * 10),
			Days:     []string{},
		}

		timer24bit := protocol.EncodeClimateTimer(immediateTimer)
		timerPayload[1] = byte(timer24bit >> 16)
		timerPayload[2] = byte(timer24bit >> 8)
		timerPayload[3] = byte(timer24bit)

		if err := cl.SetRegister(0x05, timerPayload); err != nil {
			log.Fatalf("Failed to start climate: %v", err)
		}
	} else {
		// MY2018+ approach - use register 0x1b
		state := byte(0x02)
		if err := cl.SetRegister(protocol.SetACModeRegisterMY18, []byte{state, modeCode, durationCode, 0x0}); err != nil {
			log.Fatalf("Failed to start climate: %v", err)
		}
	}

	log.Infof("✓ Climate control started: %s for %s minutes", mode, durationStr)
	cl.Close()
}

func runClimateStop(cmd *cobra.Command, args []string) {
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

	log.Infof("Connected to car, stopping climate control...")
	time.Sleep(2 * time.Second)

	if err := cl.SetRegister(protocol.SetAckPreACTermRegister, []byte{0x1}); err != nil {
		log.Fatalf("Failed to stop climate: %v", err)
	}

	log.Infof("✓ Climate control stopped")
	cl.Close()
}

func runClimateTimerSet(cmd *cobra.Command, args []string) {
	// Parse timer ID
	timerID, err := strconv.Atoi(args[0])
	if err != nil || timerID < 1 || timerID > 5 {
		log.Fatalf("Invalid timer ID: %s (must be 1-5)", args[0])
	}

	// Parse hour
	hour, err := strconv.Atoi(args[1])
	if err != nil || hour < 0 || hour > 23 {
		log.Fatalf("Invalid hour: %s (must be 0-23)", args[1])
	}

	// Parse minute
	minute, err := strconv.Atoi(args[2])
	if err != nil || minute < 0 || minute > 50 || minute%10 != 0 {
		log.Fatalf("Invalid minute: %s (must be 0, 10, 20, 30, 40, or 50)", args[2])
	}

	// Parse mode
	mode := strings.ToLower(args[3])
	if mode != "cool" && mode != "heat" && mode != "windscreen" {
		log.Fatalf("Invalid mode: %s (must be cool, heat, or windscreen)", mode)
	}

	// Parse duration
	duration, err := strconv.Atoi(args[4])
	if err != nil || (duration != 10 && duration != 20 && duration != 30) {
		log.Fatalf("Invalid duration: %s (must be 10, 20, or 30)", args[4])
	}

	// Parse days
	validDays := map[string]bool{"sun": true, "mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true}
	days := []string{}
	for _, day := range args[5:] {
		dayLower := strings.ToLower(day)
		if !validDays[dayLower] {
			log.Fatalf("Invalid day: %s (must be sun, mon, tue, wed, thu, fri, sat)", day)
		}
		days = append(days, dayLower)
	}

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

	log.Infof("Connected to car, setting timer %d...", timerID)
	time.Sleep(2 * time.Second)

	// Create timer configuration
	climateReg := &protocol.RegisterClimateTimer{}
	for i := 0; i < 5; i++ {
		climateReg.Timers[i].Enabled = false
	}

	climateReg.Timers[timerID-1] = protocol.ClimateTimer{
		Enabled:  true,
		Hour:     uint8(hour),
		Minute:   uint8(minute),
		Mode:     mode,
		Duration: uint8(duration),
		Days:     days,
	}

	msg := climateReg.Encode()
	if err := cl.SetRegister(protocol.SetClimateTimerRegister, msg.Data); err != nil {
		log.Fatalf("Failed to set timer: %v", err)
	}

	log.Infof("✓ Timer %d set: %02d:%02d %s for %d mins on %v", timerID, hour, minute, mode, duration, days)
	cl.Close()
}

func runClimateTimerClear(cmd *cobra.Command, args []string) {
	target := strings.ToLower(args[0])

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

	log.Infof("Connected to car, clearing timer(s)...")
	time.Sleep(2 * time.Second)

	if target == "all" {
		// Clear all timers
		clearData := make([]byte, 16)
		clearData[0] = 0x00
		for i := 1; i < 16; i += 3 {
			clearData[i] = 0xfe
			clearData[i+1] = 0x07
			clearData[i+2] = 0x00
		}
		clearData[15] = 0x01

		if err := cl.SetRegister(protocol.SetClimateTimerRegister, clearData); err != nil {
			log.Fatalf("Failed to clear timers: %v", err)
		}
		log.Infof("✓ All timers cleared")
	} else {
		// Clear specific timer
		timerID, err := strconv.Atoi(target)
		if err != nil || timerID < 1 || timerID > 5 {
			log.Fatalf("Invalid timer ID: %s (must be 1-5 or 'all')", target)
		}

		climateReg := &protocol.RegisterClimateTimer{}
		for i := 0; i < 5; i++ {
			climateReg.Timers[i].Enabled = false
		}

		msg := climateReg.Encode()
		if err := cl.SetRegister(protocol.SetClimateTimerRegister, msg.Data); err != nil {
			log.Fatalf("Failed to clear timer: %v", err)
		}
		log.Infof("✓ Timer %d cleared", timerID)
	}

	cl.Close()
}

func runClimateTimerList(cmd *cobra.Command, args []string) {
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

	log.Infof("Connected to car, reading timers...")

	// Listen for climate timer register
	timeout := time.After(10 * time.Second)
	var climateTimers *protocol.RegisterClimateTimer

	for {
		select {
		case <-timeout:
			log.Fatalf("Timeout waiting for timer data")
		case msg := <-cl.Recv:
			if msg.Register == protocol.ClimateTimerRegister {
				if reg, ok := msg.Reg.(*protocol.RegisterClimateTimer); ok {
					climateTimers = reg
					goto DISPLAY
				}
			}
		}
	}

DISPLAY:
	fmt.Println("\n═══════════════════════════════════════")
	fmt.Println("     PHEV Climate Timers")
	fmt.Println("═══════════════════════════════════════\n")

	for i, timer := range climateTimers.Timers {
		fmt.Printf("Timer %d: ", i+1)
		if !timer.Enabled {
			fmt.Println("DISABLED")
		} else {
			timerJSON, _ := json.MarshalIndent(timer, "  ", "  ")
			fmt.Printf("\n  %s\n", string(timerJSON))
		}
	}

	fmt.Println("\n═══════════════════════════════════════\n")
	cl.Close()
}

func init() {
	clientCmd.AddCommand(climateCmd)

	// Add subcommands
	climateCmd.AddCommand(climateStartCmd)
	climateCmd.AddCommand(climateStopCmd)
	climateCmd.AddCommand(climateTimerCmd)

	// Timer subcommands
	climateTimerCmd.AddCommand(climateTimerSetCmd)
	climateTimerCmd.AddCommand(climateTimerClearCmd)
	climateTimerCmd.AddCommand(climateTimerListCmd)
}
