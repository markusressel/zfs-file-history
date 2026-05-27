package configuration

type FileBrowserPermissionsFormat string

const (
	FileBrowserPermissionsFormatOctal    FileBrowserPermissionsFormat = "octal"
	FileBrowserPermissionsFormatSymbolic FileBrowserPermissionsFormat = "symbolic"
)

type FileBrowserConfig struct {
	Permissions FileBrowserPermissionsFormat `json:"permissions"`
}
