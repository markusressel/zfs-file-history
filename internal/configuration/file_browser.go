package configuration

type FileBrowserPermissionsFormat string

type FileBrowserOwnerFormat string

const (
	FileBrowserPermissionsFormatOctal    FileBrowserPermissionsFormat = "octal"
	FileBrowserPermissionsFormatSymbolic FileBrowserPermissionsFormat = "symbolic"

	FileBrowserOwnerFormatName FileBrowserOwnerFormat = "name"
	FileBrowserOwnerFormatID   FileBrowserOwnerFormat = "id"
	FileBrowserOwnerFormatBoth FileBrowserOwnerFormat = "both"
)

type FileBrowserConfig struct {
	Permissions FileBrowserPermissionsFormat `json:"permissions"`
	Owner       FileBrowserOwnerFormat       `json:"owner"`
}
