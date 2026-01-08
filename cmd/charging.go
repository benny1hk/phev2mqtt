/*
Copyright © 2026 Ben Buxton <bbuxton@gmail.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
package cmd

import (
	"time"

	"github.com/buxtronix/phev2mqtt/client"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// chargingCmd represents the charging command
var chargingCmd = &cobra.Command{
	Use:   "charging",
	Short: "Control battery charging",
	Long: `Control battery charging and charge timer.

Available subcommands:
  cancel-timer - Cancel charge timer`,
}

// cancelChargeTimerCmd cancels the charge timer
var cancelChargeTimerCmd = &cobra.Command{
	Use:   "cancel-timer",
	Short: "Cancel charge timer",
	Long: `Cancel the scheduled charge timer, allowing immediate charging.

Example:
  phev2mqtt client charging cancel-timer`,
	Run: runCancelChargeTimer,
}

func runCancelChargeTimer(cmd *cobra.Command, args []string) {
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

	log.Infof("Connected to car, canceling charge timer...")
	time.Sleep(2 * time.Second)

	// Send cancel commands
	if err := cl.SetRegister(0x17, []byte{0x1}); err != nil {
		log.Fatalf("Failed to cancel charge timer (step 1): %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if err := cl.SetRegister(0x17, []byte{0x11}); err != nil {
		log.Fatalf("Failed to cancel charge timer (step 2): %v", err)
	}

	log.Infof("✓ Charge timer canceled")
	cl.Close()
}

func init() {
	clientCmd.AddCommand(chargingCmd)
	chargingCmd.AddCommand(cancelChargeTimerCmd)
}
