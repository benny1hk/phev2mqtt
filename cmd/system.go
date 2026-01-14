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

	"github.com/spf13/cobra"
)

// systemCmd represents the system command
var systemCmd = &cobra.Command{
	Use:   "system",
	Short: "Show system metrics",
	Long: `Display current system metrics including CPU temperature, memory usage, CPU load, disk usage, and uptime.

Example:
  phev2mqtt system`,
	Run: runSystem,
}

func runSystem(cmd *cobra.Command, args []string) {
	metrics := collectSystemMetrics()
	displaySystemMetrics(metrics)
}

func displaySystemMetrics(m *systemMetrics) {
	fmt.Println("\n╔════════════════════════════════════════════════╗")
	fmt.Println("║         RASPBERRY PI SYSTEM METRICS            ║")
	fmt.Println("╠════════════════════════════════════════════════╣")

	// CPU Temperature
	if m.CPUTemp > 0 {
		tempIcon := "🟢"
		if m.CPUTemp > 70 {
			tempIcon = "🔴"
		} else if m.CPUTemp > 60 {
			tempIcon = "🟡"
		}
		fmt.Printf("║ CPU Temperature:    %5.1f°C %s                 ║\n", m.CPUTemp, tempIcon)
	} else {
		fmt.Println("║ CPU Temperature:    N/A                        ║")
	}

	// Memory Usage
	if m.MemoryPercent > 0 {
		memIcon := "🟢"
		if m.MemoryPercent > 90 {
			memIcon = "🔴"
		} else if m.MemoryPercent > 75 {
			memIcon = "🟡"
		}
		fmt.Printf("║ Memory Usage:       %5.1f%% %s                  ║\n", m.MemoryPercent, memIcon)
	} else {
		fmt.Println("║ Memory Usage:       N/A                        ║")
	}

	// CPU Load
	if m.CPULoad >= 0 {
		loadIcon := "🟢"
		if m.CPULoad > 90 {
			loadIcon = "🔴"
		} else if m.CPULoad > 75 {
			loadIcon = "🟡"
		}
		fmt.Printf("║ CPU Load:           %5.1f%% %s                  ║\n", m.CPULoad, loadIcon)
	} else {
		fmt.Println("║ CPU Load:           N/A                        ║")
	}

	// Disk Usage
	if m.DiskPercent > 0 {
		diskIcon := "🟢"
		if m.DiskPercent > 90 {
			diskIcon = "🔴"
		} else if m.DiskPercent > 75 {
			diskIcon = "🟡"
		}
		fmt.Printf("║ Disk Usage:         %5.1f%% %s                  ║\n", m.DiskPercent, diskIcon)
	} else {
		fmt.Println("║ Disk Usage:         N/A                        ║")
	}

	// Uptime
	if m.UptimeSeconds > 0 {
		duration := time.Duration(m.UptimeSeconds) * time.Second
		days := int(duration.Hours() / 24)
		hours := int(duration.Hours()) % 24
		minutes := int(duration.Minutes()) % 60

		uptimeStr := ""
		if days > 0 {
			uptimeStr = fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
		} else if hours > 0 {
			uptimeStr = fmt.Sprintf("%dh %dm", hours, minutes)
		} else {
			uptimeStr = fmt.Sprintf("%dm", minutes)
		}
		fmt.Printf("║ Uptime:             %-23s ║\n", uptimeStr)
	} else {
		fmt.Println("║ Uptime:             N/A                        ║")
	}

	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()
}

func init() {
	rootCmd.AddCommand(systemCmd)
}
