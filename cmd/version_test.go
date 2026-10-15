package cmd

import (
	"bytes"
	"os"
	"testing"
	"zfs-file-history/cmd/global"
	"zfs-file-history/internal/testutil"

	"github.com/pterm/pterm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: the version was only written to the log file, not to the terminal.
func TestVersionCommandPrintsTheVersion(t *testing.T) {
	testutil.Replace(t, &global.Version, "2.0.0")
	testutil.Replace(t, &global.Commit, "abc1234")
	testutil.Replace(t, &global.Date, "2026-10-03T00:00:00Z")
	t.Cleanup(func() {
		pterm.SetDefaultOutput(os.Stdout)
		rootCmd.SetArgs(nil)
	})

	tests := []struct {
		args     []string
		expected string
	}{
		{[]string{"version"}, "2.0.0\n"},
		{[]string{"version", "--long"}, "2.0.0-abc1234\n"},
		{[]string{"version", "--verbose"}, "2.0.0-abc1234-2026-10-03T00:00:00Z\n"},
	}
	for _, test := range tests {
		t.Run(test.args[len(test.args)-1], func(t *testing.T) {
			testutil.Replace(t, &long, false)
			testutil.Replace(t, &global.Verbose, false)
			var output bytes.Buffer
			pterm.SetDefaultOutput(&output)
			rootCmd.SetArgs(test.args)
			require.NoError(t, rootCmd.Execute())
			assert.Equal(t, test.expected, output.String())
		})
	}
}
