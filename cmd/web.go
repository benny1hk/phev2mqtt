/*
Copyright © 2026 Ben Buxton <bbuxton@gmail.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/buxtronix/phev2mqtt/client"
	"github.com/buxtronix/phev2mqtt/web"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Run the web UI / REST API standalone (no MQTT bridge).",
	Long: `Start an HTTP server that exposes a small web UI and REST API for
the vehicle. Owns its own connection to the PHEV. Requires Wifi reachability
to the vehicle.

Authentication uses a single user, username and bcrypt password hash stored
in the config file (~/.phev2mqtt.yaml). On first run, default credentials
are seeded (admin/admin) and you will be prompted to change them.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		address, _ := cmd.Flags().GetString("address")
		if address == "" {
			if v := viper.GetString("address"); v != "" {
				address = v
			} else {
				address = client.DefaultAddress
			}
		}
		listen, _ := cmd.Flags().GetString("web_listen")
		if listen == "" {
			if v := viper.GetString("web_listen"); v != "" {
				listen = v
			} else {
				listen = ":8888"
			}
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		driver := web.NewSnapshotDriver(address)
		go driver.Run(ctx)

		srv, err := web.NewServer(web.Config{
			Listen:   listen,
			Provider: driver,
		})
		if err != nil {
			return err
		}
		return srv.Run(ctx)
	},
}

func init() {
	rootCmd.AddCommand(webCmd)
	webCmd.Flags().String("address", client.DefaultAddress, "Address of the PHEV")
	webCmd.Flags().String("web_listen", ":8888", "Listen address for the web server")
}
