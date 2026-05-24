package configuration

type DiffMode string

const (
	DiffModeInternal DiffMode = "internal"
	DiffModeExternal DiffMode = "external"
)

type DiffConfig struct {
	Mode     DiffMode           `json:"mode"`
	External ExternalDiffConfig `json:"external"`
}

type ExternalDiffConfig struct {
	Editor ExternalDiffEditorConfig `json:"editor"`
}

type ExternalDiffEditorConfig struct {
	Path string `json:"path"`
}
