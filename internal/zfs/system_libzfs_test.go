package zfs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseSnapshotProperties(t *testing.T) {
	output := "pool/data@daily\tcreation\t1791925200\n" +
		"pool/data@daily\tused\t46747648\n" +
		"pool/data@daily\treferenced\t209721733120\n" +
		"pool/data@daily\twritten\t87625728\n" +
		"pool/data@daily\trefcompressratio\t2.01x\n" +
		"pool/data@daily\tclones\tpool/clone1,pool/clone2\n" +
		"pool/data@daily\tuserrefs\t1\n" +
		"pool/data@weekly\tclones\t-\n"

	properties := parseSnapshotProperties(output)

	assert.Equal(t, SnapshotProperties{
		CreationDate:     time.Unix(1791925200, 0),
		Used:             46747648,
		Referenced:       209721733120,
		Written:          87625728,
		CompressionRatio: 2.01,
		Clones:           2,
		Holds:            1,
	}, properties["daily"])
	assert.Equal(t, SnapshotProperties{}, properties["weekly"])
}
