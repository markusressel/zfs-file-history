package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"zfs-file-history/cmd/global"
	"zfs-file-history/internal"
	"zfs-file-history/internal/configuration"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/state"
	"zfs-file-history/internal/ui"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

// fresh starts without restoring the last session, see ui.Start.RestoreSession
var fresh bool

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "zfs-file-history",
	Short: "Easily recover snapshotted versions of files on your ZFS dataset.",
	Long:  ``,
	Args:  cobra.MaximumNArgs(1),
	// this is the default command to run when no subcommand is specified
	Run: func(cmd *cobra.Command, args []string) {
		configPath := configuration.DetectAndReadConfigFile()
		logging.Info("Using configuration file at: %s", configPath)
		configuration.LoadConfig()
		err := configuration.Validate(configPath)
		if err != nil {
			logging.Error("Config Validation Error: %v", err.Error())
			return
		}

		currentWorkingDirectory, err := os.Getwd()
		if err != nil {
			logging.Fatal("Couldn't find current working dir: %v", err)
		}
		start := ui.Start{
			Path:           currentWorkingDirectory,
			LaunchDir:      currentWorkingDirectory,
			PathGiven:      len(args) > 0,
			RestoreSession: configuration.CurrentConfig.RestoreSession && !fresh,
		}
		if start.PathGiven {
			start.Path, err = filepath.Abs(args[0])
			if err != nil {
				logging.Fatal("Couldn't resolve path: %v", err)
			}
		}

		state.Init()
		internal.RunApplication(start)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&global.CfgFile, "config", "c", "", "config file (default is $HOME/.zfs-file-history.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&global.NoColor, "no-color", "", false, "Disable all terminal output coloration")
	rootCmd.PersistentFlags().BoolVarP(&global.NoStyle, "no-style", "", false, "Disable all terminal output styling")
	rootCmd.PersistentFlags().BoolVarP(&global.Verbose, "verbose", "v", false, "More verbose output")
	rootCmd.Flags().BoolVarP(&fresh, "fresh", "", false, "Start in the given path or working directory, without restoring the last session")
}

func setupUi() {
	logging.SetDebugEnabled(global.Verbose)

	if global.NoColor {
		pterm.DisableColor()
	}
	if global.NoStyle {
		pterm.DisableStyling()
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.OnInitialize(func() {
		configuration.InitConfig(global.CfgFile)
		setupUi()
	})

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
