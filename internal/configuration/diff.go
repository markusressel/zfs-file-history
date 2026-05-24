package configuration

type DiffMode string

const (
	DiffModeInternal DiffMode = "internal"
	DiffModeExternal DiffMode = "external"
)

type DiffConfig struct {
	Mode     DiffMode            `json:"mode"`
	External *ExternalDiffConfig `json:"external,omitempty"`
}

type ExternalDiffConfig struct {
	Path        string   `json:"path"`
	Args        []string `json:"args"`
	WrapInPager bool     `json:"wrapInPager"`
}
