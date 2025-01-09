package cmd

import "github.com/spf13/cobra"

var (
	Version   = "dev"
	BuildDate = "unknown"

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Shows version of rtorrent-exporter",
		Run:   RunVersion,
	}
)

func RunVersion(cmd *cobra.Command, args []string) {
	cmd.Printf("rtorrent-exporter version: %s\n", Version)
	cmd.Printf("Built on: %s\n", BuildDate)
}
