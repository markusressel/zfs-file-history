package file_browser

import "os/exec"

type ExternalDiffViewerConfig struct {
	Path        string
	Args        []string
	WrapInPager bool
}

func (c ExternalDiffViewerConfig) computeRunArgs() []string {
	args := make([]string, len(c.Args))
	copy(args, c.Args)
	return args
}

func (c ExternalDiffViewerConfig) IsAvailable() bool {
	_, err := exec.LookPath(c.Path)
	return err == nil
}
