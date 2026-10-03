package file_browser

import (
	"slices"
	"testing"
	"time"
	"zfs-file-history/internal/folder_listing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// After loading a folder, the file browser announces it, and its entries are the folder as it is now: the same as
// reading the folder, without reading it again.
func TestFileBrowser_WorkingCopyListing(t *testing.T) {
	ft := newFileBrowserTest(t, "")
	loaded := make(chan string, 10)
	ft.fileBrowser.Events.Subscribe(func(event Event) {
		if e, ok := event.(EntriesLoadedEvent); ok {
			loaded <- e.Path
		}
	})
	onUiThread(t, ft.app, func() { ft.fileBrowser.SetPath(ft.dir, true) })
	select {
	case path := <-loaded:
		assert.Equal(t, ft.dir, path)
	case <-time.After(3 * time.Second):
		t.Fatal("no EntriesLoadedEvent")
	}

	var path string
	var listing folder_listing.Listing
	onUiThread(t, ft.app, func() { path, listing = ft.fileBrowser.WorkingCopyListing() })
	assert.Equal(t, ft.dir, path)
	var names []string
	for name := range listing.Entries {
		names = append(names, name)
	}
	slices.Sort(names)
	assert.Equal(t, []string{"a.txt", "b.txt", "c.log", "sub"}, names)
	require.True(t, listing.Equal(folder_listing.Read(ft.dir)), "the same as reading the folder")
}
