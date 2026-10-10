package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestInitConfigAndDefaults(t *testing.T) {
	viper.Reset()
	InitConfig("")

	assert.Equal(t, DiffModeExternal, DiffMode(viper.GetString("Diff.Mode")))
	assert.Equal(t, string(FileBrowserPermissionsFormatSymbolic), viper.GetString("FileBrowser.Permissions"))
	assert.Equal(t, string(FileBrowserOwnerFormatName), viper.GetString("FileBrowser.Owner"))
	assert.Equal(t, string(FileBrowserFilterOnDirectoryChangeKeep), viper.GetString("FileBrowser.FilterOnDirectoryChange"))
	assert.Equal(t, "localhost", viper.GetString("Profiling.Host"))
	assert.Equal(t, 6060, viper.GetInt("Profiling.Port"))

	LoadConfig()
	assert.Equal(t, DiffModeExternal, CurrentConfig.Diff.Mode)
	assert.Equal(t, FileBrowserPermissionsFormatSymbolic, CurrentConfig.FileBrowser.Permissions)
	assert.Equal(t, FileBrowserOwnerFormatName, CurrentConfig.FileBrowser.Owner)
	assert.True(t, CurrentConfig.RestoreSession)
}

func TestInitConfigWithCustomFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom.yaml")
	configContent := `
restoreSession: false
diff:
  mode: internal
fileBrowser:
  permissions: octal
  owner: id
  filterOnDirectoryChange: clear
profiling:
  enabled: true
  host: 0.0.0.0
  port: 8080
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	assert.NoError(t, err)

	viper.Reset()
	InitConfig(configPath)

	detectedPath := DetectAndReadConfigFile()
	assert.Equal(t, configPath, detectedPath)
	assert.Equal(t, configPath, GetFilePath())

	LoadConfig()
	assert.Equal(t, DiffModeInternal, CurrentConfig.Diff.Mode)
	assert.Equal(t, FileBrowserPermissionsFormatOctal, CurrentConfig.FileBrowser.Permissions)
	assert.Equal(t, FileBrowserOwnerFormatID, CurrentConfig.FileBrowser.Owner)
	assert.Equal(t, FileBrowserFilterOnDirectoryChangeClear, CurrentConfig.FileBrowser.FilterOnDirectoryChange)
	assert.True(t, CurrentConfig.Profiling.Enabled)
	assert.Equal(t, "0.0.0.0", CurrentConfig.Profiling.Host)
	assert.Equal(t, 8080, CurrentConfig.Profiling.Port)
	assert.False(t, CurrentConfig.RestoreSession)
}

func TestFileBrowserConfig_KeepsFilterOnDirectoryChange(t *testing.T) {
	cfgKeep := FileBrowserConfig{
		FilterOnDirectoryChange: FileBrowserFilterOnDirectoryChangeKeep,
	}
	assert.True(t, cfgKeep.KeepsFilterOnDirectoryChange())

	cfgEmpty := FileBrowserConfig{}
	assert.True(t, cfgEmpty.KeepsFilterOnDirectoryChange())

	cfgClear := FileBrowserConfig{
		FilterOnDirectoryChange: FileBrowserFilterOnDirectoryChangeClear,
	}
	assert.False(t, cfgClear.KeepsFilterOnDirectoryChange())
}
