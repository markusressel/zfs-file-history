package configuration

import (
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
	"os"
	path2 "path"
	"zfs-file-history/internal/logging"
)

type Configuration struct {
	Profiling ProfilingConfig `json:"profiling"`
}

var CurrentConfig Configuration

// InitConfig reads in config file and ENV variables if set.
func InitConfig(cfgFile string) {
	viper.SetConfigName("zfs-file-history")

	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			logging.Error("Path Error: Couldn't detect home directory: %v", err)
			os.Exit(1)
		}

		viper.AddConfigPath(".")
		viper.AddConfigPath(home)
		viper.AddConfigPath(path2.Join(home, ".config"))
		viper.AddConfigPath(path2.Join(home, ".config", "zfs-file-history"))
		viper.AddConfigPath("/etc/zfs-file-history/")
	}

	viper.AutomaticEnv() // read in environment variables that match

	setDefaultValues()
}

func setDefaultValues() {
	viper.SetDefault("Profiling", ProfilingConfig{
		Enabled: false,
		Host:    "localhost",
		Port:    6060,
	})
	viper.SetDefault("Profiling.Host", "localhost")
	viper.SetDefault("Profiling.Port", 6060)
}

// DetectAndReadConfigFile detects the path of the first existing config file
func DetectAndReadConfigFile() string {
	_ = readInConfig()
	return GetFilePath()
}

// readInConfig reads and parses the config file
func readInConfig() error {
	return viper.ReadInConfig()
}

// GetFilePath this is only populated _after_ readInConfig()
func GetFilePath() string {
	return viper.ConfigFileUsed()
}

func LoadConfig() {
	// load default configuration values
	err := viper.Unmarshal(&CurrentConfig)
	if err != nil {
		logging.Fatal("unable to decode into struct, %v", err)
	}
}
