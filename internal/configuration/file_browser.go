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

// KeepsFilterOnDirectoryChange returns whether an active filter is kept when changing directories. By default (also
// if the setting is empty) it is cleared: the filter is for finding an entry, like in most file managers.
func (c FileBrowserConfig) KeepsFilterOnDirectoryChange() bool {
	return c.FilterOnDirectoryChange == FileBrowserFilterOnDirectoryChangeKeep
}
