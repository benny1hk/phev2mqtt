/*
Copyright © 2026 Ben Buxton <bbuxton@gmail.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
package cmd

import (
	"strings"
	"time"

	"github.com/buxtronix/phev2mqtt/client"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// lightsCmd represents the lights command
var lightsCmd = &cobra.Command{
	Use:   "lights",
	Short: "Control vehicle lights",
	Long: `Control parking lights and headlights.

Available subcommands:
  parking    - Control parking lights
  headlights - Control headlights`,
}

// parkingCmd controls parking lights
var parkingCmd = &cobra.Command{
	Use:   "parking [on|off]",
	Short: "Control parking lights",
	Long: `Turn parking lights on or off.

Examples:
  phev2mqtt client lights parking on
  phev2mqtt client lights parking off`,
	Args: cobra.ExactArgs(1),
	Run:  runParking,
}

// headlightsCmd controls headlights
var headlightsCmd = &cobra.Command{
	Use:   "headlights [on|off]",
	Short: "Control headlights",
	Long: `Turn headlights on or off.

Examples:
  phev2mqtt client lights headlights on
  phev2mqtt client lights headlights off`,
	Args: cobra.ExactArgs(1),
	Run:  runHeadlights,
}

func runParking(cmd *cobra.Command, args []string) {
	state := strings.ToLower(args[0])
	if state != "on" && state != "off" {
		log.Fatalf("Invalid state: %s (must be 'on' or 'off')", state)
	}

	var value byte
	if state == "on" {
		value = 0x1
	} else {
		value = 0x2
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

	log.Infof("Connected to car, setting parking lights %s...", state)
	time.Sleep(2 * time.Second)

	if err := cl.SetRegister(0x0b, []byte{value}); err != nil {
		log.Fatalf("Failed to set parking lights: %v", err)
	}

	log.Infof("✓ Parking lights turned %s", state)
	cl.Close()
}

func runHeadlights(cmd *cobra.Command, args []string) {
	state := strings.ToLower(args[0])
	if state != "on" && state != "off" {
		log.Fatalf("Invalid state: %s (must be 'on' or 'off')", state)
	}

	var value byte
	if state == "on" {
		value = 0x1
	} else {
		value = 0x2
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

	log.Infof("Connected to car, setting headlights %s...", state)
	time.Sleep(2 * time.Second)

	if err := cl.SetRegister(0x0a, []byte{value}); err != nil {
		log.Fatalf("Failed to set headlights: %v", err)
	}

	log.Infof("✓ Headlights turned %s", state)
	cl.Close()
}

func init() {
	clientCmd.AddCommand(lightsCmd)
	lightsCmd.AddCommand(parkingCmd)
	lightsCmd.AddCommand(headlightsCmd)
}
