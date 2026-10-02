package zfs

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProperties(t *testing.T) {
	output := "type\tfilesystem\t-\n" +
		"compression\tzstd\tinherited from rpool\n" +
		"canmount\ton\tlocal\n" +
		"com.sun:auto-snapshot\ttrue with spaces\tlocal\n" +
		"quota\tnone\tdefault\n"

	properties, err := parseProperties(output)

	require.NoError(t, err)
	assert.Equal(t, []*Property{
		{Name: "type", Value: "filesystem", Source: "-"},
		{Name: "compression", Value: "zstd", Source: "inherited from rpool"},
		{Name: "canmount", Value: "on", Source: "local"},
		{Name: "com.sun:auto-snapshot", Value: "true with spaces", Source: "local"},
		{Name: "quota", Value: "none", Source: "default"},
	}, properties)

	_, err = parseProperties("broken line")
	assert.Error(t, err)
}

func TestProperty(t *testing.T) {
	readOnly := Property{Name: "used", Value: "1G", Source: "-"}
	inherited := Property{Name: "compression", Value: "zstd", Source: "inherited from rpool"}
	local := Property{Name: "com.sun:auto-snapshot", Value: "true", Source: "local"}

	assert.False(t, readOnly.IsEditable())
	assert.True(t, inherited.IsEditable())
	assert.False(t, inherited.IsLocal())
	assert.True(t, local.IsLocal())
	assert.True(t, local.IsUserProperty())
	assert.False(t, inherited.IsUserProperty())
	assert.Equal(t, "compression", inherited.TableRowId())
}

func TestPropertyCommands(t *testing.T) {
	assert.Equal(t, []string{"zfs", "set", "compression=zstd-3", "pool/data"}, SetPropertyCommand("pool/data", "compression", "zstd-3"))
	assert.Equal(t, []string{"zfs", "set", "com.sun:note=two words", "pool/data"}, SetPropertyCommand("pool/data", "com.sun:note", "two words"))
	assert.Equal(t, []string{"zfs", "inherit", "compression", "pool/data"}, InheritPropertyCommand("pool/data", "compression"))
}

func TestSetProperty_Invalid(t *testing.T) {
	// rejected before any zfs process is spawned
	for _, name := range []string{"", "a=b", "two words"} {
		err := SetProperty("pool/data", name, "x")
		assert.Error(t, err, name)
		assert.False(t, errors.Is(err, ErrPermissionDenied))
	}
	assert.Error(t, SetProperty("pool/data", "com.sun:note", "line\nbreak"))
	assert.Error(t, InheritProperty("pool/data", ""))
}
