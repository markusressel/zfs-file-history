package configuration

import "testing"

func TestValidateConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *Configuration
		path    string
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "profiling disabled ignores host and port",
			config: &Configuration{Profiling: ProfilingConfig{
				Enabled: false,
				Host:    "",
				Port:    0,
			}},
			wantErr: false,
		},
		{
			name: "profiling enabled with valid host and port",
			config: &Configuration{Profiling: ProfilingConfig{
				Enabled: true,
				Host:    "127.0.0.1",
				Port:    6060,
			}},
			wantErr: false,
		},
		{
			name: "profiling enabled with empty host",
			config: &Configuration{Profiling: ProfilingConfig{
				Enabled: true,
				Host:    " ",
				Port:    6060,
			}},
			wantErr: true,
		},
		{
			name: "profiling enabled with invalid low port",
			config: &Configuration{Profiling: ProfilingConfig{
				Enabled: true,
				Host:    "localhost",
				Port:    0,
			}},
			wantErr: true,
		},
		{
			name: "profiling enabled with invalid high port",
			config: &Configuration{Profiling: ProfilingConfig{
				Enabled: true,
				Host:    "localhost",
				Port:    70000,
			}},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateConfig(tc.config, tc.path)
			if tc.wantErr && err == nil {
				t.Fatalf("expected validation error but got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no validation error, got: %v", err)
			}
		})
	}
}
