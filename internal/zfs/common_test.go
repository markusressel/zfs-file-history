package zfs

import (
	"testing"

	golibzfs "github.com/kraudcloud/go-libzfs"
	"github.com/stretchr/testify/assert"
)

func TestFindSnapshot(t *testing.T) {
	snapshots := []golibzfs.Dataset{
		{
			Properties: map[golibzfs.Prop]golibzfs.Property{
				golibzfs.DatasetPropName: {Value: "pool/ds1@snap1"},
			},
		},
		{
			Properties: map[golibzfs.Prop]golibzfs.Property{
				golibzfs.DatasetPropName: {Value: "pool/ds1@snap2"},
			},
		},
		{
			Properties: map[golibzfs.Prop]golibzfs.Property{
				golibzfs.DatasetPropName: {Value: "invalid-snapshot-name"},
			},
		},
	}

	tests := []struct {
		name     string
		snapName string
		wantName string
		found    bool
	}{
		{
			name:     "Find First",
			snapName: "snap1",
			wantName: "pool/ds1@snap1",
			found:    true,
		},
		{
			name:     "Find Second",
			snapName: "snap2",
			wantName: "pool/ds1@snap2",
			found:    true,
		},
		{
			name:     "Not Found",
			snapName: "snap3",
			found:    false,
		},
		{
			name:     "Ignore Invalid Name",
			snapName: "invalid",
			found:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSnapshot(snapshots, tt.snapName)
			if tt.found {
				assert.NotNil(t, got)
				assert.Equal(t, tt.wantName, got.Properties[golibzfs.DatasetPropName].Value)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}
