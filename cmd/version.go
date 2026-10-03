package cmd

import (
	"zfs-file-history/cmd/global"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var long bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of zfs-file-history",
	Long:  `All software has versions. This is zfs-file-history's`,
	Run: func(cmd *cobra.Command, args []string) {
		if global.Verbose {
			pterm.Printfln("%s-%s-%s", global.Version, global.Commit, global.Date)
		} else if long {
			pterm.Printfln("%s-%s", global.Version, global.Commit)
		} else {
			pterm.Printfln("%s", global.Version)
		}
	},
}

func init() {
	versionCmd.Flags().BoolVarP(&long, "long", "l", false, "Show the long version")

	rootCmd.AddCommand(versionCmd)
}
