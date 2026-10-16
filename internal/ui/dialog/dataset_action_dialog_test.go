package dialog

import (
	"testing"
	"zfs-file-history/internal/zfs"

	"github.com/stretchr/testify/assert"
)

func TestBuildMountDialogOptions(t *testing.T) {
	tests := []struct {
		name    string
		dataset zfs.DatasetListEntry
		ids     []DialogActionId
	}{
		{name: "mounted", dataset: zfs.DatasetListEntry{CanMount: "on", Mountpoint: "/data", MountPath: "/data"},
			ids: []DialogActionId{DatasetDialogUnmountActionId}},
		{name: "not mounted", dataset: zfs.DatasetListEntry{CanMount: "noauto", Mountpoint: "/data"},
			ids: []DialogActionId{DatasetDialogMountActionId}},
		{name: "canmount off", dataset: zfs.DatasetListEntry{CanMount: "off", Mountpoint: "/data"}},
		{name: "mountpoint none", dataset: zfs.DatasetListEntry{CanMount: "on", Mountpoint: "none"}},
		{name: "mountpoint legacy", dataset: zfs.DatasetListEntry{CanMount: "on", Mountpoint: "legacy"}},
		{name: "key loaded", dataset: zfs.DatasetListEntry{CanMount: "on", Mountpoint: "/data", MountPath: "/data",
			KeyStatus: "available", EncryptionRoot: "pool/data"},
			ids: []DialogActionId{DatasetDialogUnmountActionId, DatasetDialogUnloadKeyActionId}},
		{name: "key not loaded", dataset: zfs.DatasetListEntry{CanMount: "on", Mountpoint: "/data",
			KeyStatus: "unavailable", EncryptionRoot: "pool"},
			ids: []DialogActionId{DatasetDialogMountActionId, DatasetDialogLoadKeyActionId}},
		{name: "key not loaded, not mountable", dataset: zfs.DatasetListEntry{CanMount: "off", Mountpoint: "/data",
			KeyStatus: "unavailable", EncryptionRoot: "pool"},
			ids: []DialogActionId{DatasetDialogLoadKeyActionId}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ids []DialogActionId
			for _, option := range buildMountDialogOptions(&test.dataset) {
				ids = append(ids, option.Id)
			}
			assert.Equal(t, test.ids, ids)
		})
	}
	// mounting loads the key if it is not loaded
	locked := buildMountDialogOptions(&zfs.DatasetListEntry{CanMount: "on", Mountpoint: "/data", KeyStatus: "unavailable", EncryptionRoot: "pool"})
	assert.Equal(t, "🔓 Load key and mount", locked[0].Name)
	assert.Equal(t, "Load the key of pool and mount the dataset at /data (zfs mount -l)", locked[0].Description)
}
