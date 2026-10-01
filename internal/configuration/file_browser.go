package configuration

type FileBrowserPermissionsFormat string

type FileBrowserOwnerFormat string

// FileBrowserFilterOnDirectoryChange defines what happens to an active filter when changing directories.
type FileBrowserFilterOnDirectoryChange string

const (
	FileBrowserPermissionsFormatOctal    FileBrowserPermissionsFormat = "octal"
	FileBrowserPermissionsFormatSymbolic FileBrowserPermissionsFormat = "symbolic"

	FileBrowserOwnerFormatName FileBrowserOwnerFormat = "name"
	FileBrowserOwnerFormatID   FileBrowserOwnerFormat = "id"
	FileBrowserOwnerFormatBoth FileBrowserOwnerFormat = "both"

	FileBrowserFilterOnDirectoryChangeKeep  FileBrowserFilterOnDirectoryChange = "keep"
	FileBrowserFilterOnDirectoryChangeClear FileBrowserFilterOnDirectoryChange = "clear"
)

type FileBrowserConfig struct {
	Permissions             FileBrowserPermissionsFormat       `json:"permissions"`
	Owner                   FileBrowserOwnerFormat             `json:"owner"`
	FilterOnDirectoryChange FileBrowserFilterOnDirectoryChange `json:"filterOnDirectoryChange"`
}

// KeepsFilterOnDirectoryChange returns whether an active filter is kept when changing directories.
// This is the default, also if the setting is empty.
func (c FileBrowserConfig) KeepsFilterOnDirectoryChange() bool {
	return c.FilterOnDirectoryChange != FileBrowserFilterOnDirectoryChangeClear
}
