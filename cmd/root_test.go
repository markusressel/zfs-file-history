package cmd

import (
	"testing"
	"zfs-file-history/cmd/global"
	"zfs-file-history/internal/testutil"

	"github.com/stretchr/testify/assert"
)

func TestRootCmd_FlagsAndArgs(t *testing.T) {
	flags := []string{"config", "no-color", "no-style", "verbose"}
	for _, f := range flags {
		assert.NotNil(t, rootCmd.PersistentFlags().Lookup(f), "missing persistent flag %s", f)
	}

	assert.NoError(t, rootCmd.Args(rootCmd, []string{}))
	assert.NoError(t, rootCmd.Args(rootCmd, []string{"/some/path"}))
	assert.Error(t, rootCmd.Args(rootCmd, []string{"/path1", "/path2"}))
}

func TestSetupUi(t *testing.T) {
	testutil.Replace(t, &global.Verbose, true)
	testutil.Replace(t, &global.NoColor, true)
	testutil.Replace(t, &global.NoStyle, true)
	setupUi()

	testutil.Replace(t, &global.Verbose, false)
	testutil.Replace(t, &global.NoColor, false)
	testutil.Replace(t, &global.NoStyle, false)
	setupUi()
}

func TestSetupCmdRegistration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"setup"})
	assert.NoError(t, err)
	assert.NotNil(t, cmd)
	assert.Equal(t, "setup", cmd.Name())
}
