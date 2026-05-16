package configuration

import (
	"fmt"
	"strings"
)

func Validate(configPath string) error {
	return validateConfig(&CurrentConfig, configPath)
}

func validateConfig(config *Configuration, path string) error {
	if config == nil {
		return fmt.Errorf("invalid config: config is nil")
	}

	prefix := "invalid config"
	if path != "" {
		prefix = fmt.Sprintf("invalid config (%s)", path)
	}

	err := validateProfiling(config.Profiling)
	if err != nil {
		return fmt.Errorf("%s: %w", prefix, err)
	}

	return nil
}

func validateProfiling(profiling ProfilingConfig) error {
	if !profiling.Enabled {
		return nil
	}

	host := strings.TrimSpace(profiling.Host)
	if host == "" {
		return fmt.Errorf("profiling.host must not be empty when profiling is enabled")
	}

	if profiling.Port < 1 || profiling.Port > 65535 {
		return fmt.Errorf("profiling.port must be between 1 and 65535")
	}

	return nil
}
