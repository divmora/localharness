package main

import (
	"encoding/json"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/divmora/localharness/internal/config"
	"github.com/divmora/localharness/internal/daemon"
)

// VersionOutput holds structured version details for JSON output.
type VersionOutput struct {
	Client VersionClientInfo  `json:"client"`
	Daemon *VersionDaemonInfo `json:"daemon,omitempty"`
}

// VersionClientInfo contains CLI binary build details.
type VersionClientInfo struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// VersionDaemonInfo contains background daemon process status.
type VersionDaemonInfo struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	Port    int    `json:"port,omitempty"`
	Version string `json:"version,omitempty"`
}

// formatVersion normalizes a version string so it consistently starts with 'v'.
func formatVersion(ver string) string {
	ver = strings.TrimSpace(ver)
	if ver == "" {
		return "v0.0.0-dev"
	}
	if !strings.HasPrefix(ver, "v") {
		return "v" + ver
	}
	return ver
}

// newVersionCommand creates the 'version' subcommand for lhctl.
func newVersionCommand() *cobra.Command {
	var (
		shortFlag bool
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Display version and build information for lhctl",
		RunE: func(cmd *cobra.Command, args []string) error {
			ver := formatVersion(config.HarnessVersion)

			if shortFlag {
				cmd.Println(ver)
				return nil
			}

			daemonRunning, daemonInfo, _ := daemon.IsDaemonRunning()

			if jsonFlag {
				out := VersionOutput{
					Client: VersionClientInfo{
						Version:   ver,
						GoVersion: runtime.Version(),
						OS:        runtime.GOOS,
						Arch:      runtime.GOARCH,
					},
				}
				if daemonRunning && daemonInfo != nil {
					out.Daemon = &VersionDaemonInfo{
						Running: true,
						PID:     daemonInfo.PID,
						Port:    daemonInfo.Port,
						Version: formatVersion(daemonInfo.Version),
					}
				} else {
					out.Daemon = &VersionDaemonInfo{
						Running: false,
					}
				}
				data, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return err
				}
				cmd.Println(string(data))
				return nil
			}

			cmd.Printf("lhctl version %s (%s/%s, %s)\n", ver, runtime.GOOS, runtime.GOARCH, runtime.Version())
			if daemonRunning && daemonInfo != nil {
				cmd.Printf("LocalHarness daemon: running (PID %d, Port %d, %s)\n",
					daemonInfo.PID, daemonInfo.Port, formatVersion(daemonInfo.Version))
			} else {
				cmd.Println("LocalHarness daemon: not running")
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&shortFlag, "short", "s", false, "Print only the version number")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output version information as JSON")

	return cmd
}
