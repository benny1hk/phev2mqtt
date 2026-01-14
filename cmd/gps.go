/*
Copyright © 2026 Ben Buxton <bbuxton@gmail.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
package cmd

import (
	"bufio"
	"fmt"
	"time"

	"github.com/adrianmo/go-nmea"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// gpsCmd represents the gps command
var gpsCmd = &cobra.Command{
	Use:   "gps",
	Short: "Show GPS location data",
	Long: `Display current GPS location data from USB GPS receiver.

Reads from /dev/ttyUSB0 or /dev/ttyACM0 and shows real-time GPS data.

Example:
  phev2mqtt gps`,
	Run: runGPS,
}

func runGPS(cmd *cobra.Command, args []string) {
	fmt.Println("\n╔════════════════════════════════════════════════╗")
	fmt.Println("║              GPS LOCATION DATA                 ║")
	fmt.Println("╠════════════════════════════════════════════════╣")
	fmt.Println("║ Connecting to GPS receiver...                  ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()

	port, err := openGPSSerial()
	if err != nil {
		log.Fatalf("Failed to open GPS: %v\nMake sure GPS is connected to /dev/ttyUSB0 or /dev/ttyACM0", err)
	}
	defer port.Close()

	fmt.Println("GPS connected! Waiting for fix...")
	fmt.Println("(Press Ctrl+C to exit)")
	fmt.Println()

	scanner := bufio.NewScanner(port)
	timeout := time.After(30 * time.Second)

	var lat, lon, alt, speed, heading float64
	var sats int
	var fixQuality string
	hasRMC := false
	hasGGA := false

	for {
		select {
		case <-timeout:
			fmt.Println("\n⚠️  Timeout: No GPS fix acquired after 30 seconds")
			fmt.Println("   Make sure GPS has clear view of sky")
			return
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					log.Errorf("Scanner error: %v", err)
				}
				return
			}

			line := scanner.Text()
			s, err := nmea.Parse(line)
			if err != nil {
				continue
			}

			switch msg := s.(type) {
			case nmea.RMC:
				if msg.Validity == "A" {
					lat = msg.Latitude
					lon = msg.Longitude
					speed = msg.Speed * 1.852 // knots to km/h
					heading = msg.Course
					hasRMC = true
				}

			case nmea.GGA:
				if msg.FixQuality != "0" {
					alt = msg.Altitude
					sats = int(msg.NumSatellites)

					switch msg.FixQuality {
					case "1":
						fixQuality = "GPS fix"
					case "2":
						fixQuality = "DGPS fix"
					case "4":
						fixQuality = "RTK fixed"
					case "5":
						fixQuality = "RTK float"
					default:
						fixQuality = fmt.Sprintf("Quality %s", msg.FixQuality)
					}
					hasGGA = true
				}
			}

			// Display data when we have both RMC and GGA
			if hasRMC && hasGGA {
				displayGPSData(lat, lon, alt, speed, heading, sats, fixQuality)
				return
			}
		}
	}
}

func displayGPSData(lat, lon, alt, speed, heading float64, sats int, quality string) {
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║              GPS LOCATION DATA                 ║")
	fmt.Println("╠════════════════════════════════════════════════╣")

	// Position
	latDir := "N"
	if lat < 0 {
		latDir = "S"
		lat = -lat
	}
	lonDir := "E"
	if lon < 0 {
		lonDir = "W"
		lon = -lon
	}

	fmt.Printf("║ Latitude:           %9.6f° %s                ║\n", lat, latDir)
	fmt.Printf("║ Longitude:          %9.6f° %s                ║\n", lon, lonDir)
	fmt.Printf("║ Altitude:           %8.1f m                  ║\n", alt)

	// Movement
	fmt.Println("╟────────────────────────────────────────────────╢")

	speedIcon := "🚗"
	if speed > 80 {
		speedIcon = "🏎️"
	} else if speed < 5 {
		speedIcon = "🅿️"
	}

	fmt.Printf("║ Speed:              %6.1f km/h %s             ║\n", speed, speedIcon)

	// Direction
	direction := ""
	if heading >= 337.5 || heading < 22.5 {
		direction = "N"
	} else if heading >= 22.5 && heading < 67.5 {
		direction = "NE"
	} else if heading >= 67.5 && heading < 112.5 {
		direction = "E"
	} else if heading >= 112.5 && heading < 157.5 {
		direction = "SE"
	} else if heading >= 157.5 && heading < 202.5 {
		direction = "S"
	} else if heading >= 202.5 && heading < 247.5 {
		direction = "SW"
	} else if heading >= 247.5 && heading < 292.5 {
		direction = "W"
	} else {
		direction = "NW"
	}

	fmt.Printf("║ Heading:            %6.1f° (%s)                ║\n", heading, direction)

	// GPS Quality
	fmt.Println("╟────────────────────────────────────────────────╢")

	satIcon := "🛰️"
	if sats < 4 {
		satIcon = "⚠️"
	} else if sats >= 8 {
		satIcon = "✅"
	}

	fmt.Printf("║ Satellites:         %2d %s                      ║\n", sats, satIcon)
	fmt.Printf("║ Fix Quality:        %-23s ║\n", quality)

	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()

	// Google Maps link
	if lat != 0 && lon != 0 {
		realLat := lat
		realLon := lon
		if latDir == "S" {
			realLat = -realLat
		}
		if lonDir == "W" {
			realLon = -realLon
		}
		fmt.Printf("📍 View on map: https://www.google.com/maps?q=%.6f,%.6f\n\n", realLat, realLon)
	}
}

func init() {
	rootCmd.AddCommand(gpsCmd)
}
