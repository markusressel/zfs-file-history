package dialog

import (
	"context"
	"sync"
	"zfs-file-history/internal/zfs"
)

// PoolHistoryCache keeps the history of the pools read for the PoolHistoryDialog while the application runs. zpool
// takes many seconds to print the history of a large pool (it can be hundreds of MB), so a dialog shown again shows
// the history read before right away, while it is read again in the background (see Watch): ZFS cannot tell what was
// added since. A read goes on when the dialog is closed, so it is done sooner when the dialog is shown again.
type PoolHistoryCache struct {
	// read reads the history of a pool, see zfs.PoolHistory
	read func(ctx context.Context, pool string, progress func(zfs.PoolHistoryProgress)) ([]zfs.PoolHistoryEntry, error)

	mutex sync.Mutex
	// pools are the histories of the pools by name. Guarded by mutex.
	pools map[string]*cachedPoolHistory
	// nextListener is the id of the next listener of Watch. Guarded by mutex.
	nextListener int
}

// cachedPoolHistory is the history of a pool in the PoolHistoryCache, and its read in progress. Guarded by the mutex of
// the cache.
type cachedPoolHistory struct {
	// state is what the listeners were told last
	state PoolHistoryState
	// listeners are told about each change of state, by the id Watch returned
	listeners map[int]func(PoolHistoryState)
}

// PoolHistoryState is the history of a pool as far as it is known: the last one read, and whether it is read again.
type PoolHistoryState struct {
	// Version counts the changes of the state, so a listener can tell an older state from a newer one
	Version uint64
	// History is the history read last, nil if it was not read yet
	History *PoolHistory
	// Reading is set while the history is read
	Reading bool
	// Progress is how far the read got
	Progress zfs.PoolHistoryProgress
	// Expected is the size of the history read last (see zfs.PoolHistoryProgress.Bytes), 0 if not known: about the
	// size of the one read now
	Expected int64
	// Err is why the last read failed, nil if it did not
	Err error
}

// PoolHistory is the history of a pool, read at once, with its records grouped (see zfs.GroupPoolHistory).
type PoolHistory struct {
	// Entries are the records, oldest first
	Entries []zfs.PoolHistoryEntry
	// Groups are the groups of the records, oldest first
	Groups []*zfs.PoolHistoryGroup
	// GroupOf is the group of each record
	GroupOf map[*zfs.PoolHistoryEntry]*zfs.PoolHistoryGroup
}

// NewPoolHistoryCache creates an empty cache, which reads the histories with read (see zfs.PoolHistory).
func NewPoolHistoryCache(read func(ctx context.Context, pool string, progress func(zfs.PoolHistoryProgress)) ([]zfs.PoolHistoryEntry, error)) *PoolHistoryCache {
	return &PoolHistoryCache{read: read, pools: map[string]*cachedPoolHistory{}}
}

// Watch returns the history of the pool as far as it is known, reads it again unless that is in progress already, and
// tells listener about each change (from the goroutine of the read, in order) until stop is called. May be called
// on any thread.
func (cache *PoolHistoryCache) Watch(pool string, listener func(PoolHistoryState)) (current PoolHistoryState, stop func()) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cached := cache.pools[pool]
	if cached == nil {
		cached = &cachedPoolHistory{listeners: map[int]func(PoolHistoryState){}}
		cache.pools[pool] = cached
	}
	id := cache.nextListener
	cache.nextListener++
	cached.listeners[id] = listener
	if !cached.state.Reading {
		cached.state.Reading, cached.state.Progress = true, zfs.PoolHistoryProgress{}
		cached.state.Version++
		go cache.readPool(pool, cached)
	}
	stop = func() {
		cache.mutex.Lock()
		defer cache.mutex.Unlock()
		delete(cached.listeners, id)
	}
	return cached.state, stop
}

// readPool reads the history of the pool, groups it, keeps it and tells the listeners. Runs in the background.
func (cache *PoolHistoryCache) readPool(pool string, cached *cachedPoolHistory) {
	entries, err := cache.read(context.Background(), pool, func(progress zfs.PoolHistoryProgress) {
		cache.update(cached, func(state *PoolHistoryState) { state.Progress = progress })
	})
	var history *PoolHistory
	if err == nil {
		history = groupPoolHistory(entries)
	}
	cache.update(cached, func(state *PoolHistoryState) {
		state.Reading, state.Err = false, err
		if history != nil {
			state.History, state.Expected = history, state.Progress.Bytes
		}
	})
}

// update changes the state of the history of a pool, and tells its listeners. They are called outside the lock, so
// they may call Watch or stop, in the order of the updates: updates come from the one read of the pool.
func (cache *PoolHistoryCache) update(cached *cachedPoolHistory, change func(state *PoolHistoryState)) {
	cache.mutex.Lock()
	change(&cached.state)
	cached.state.Version++
	state := cached.state
	listeners := make([]func(PoolHistoryState), 0, len(cached.listeners))
	for _, listener := range cached.listeners {
		listeners = append(listeners, listener)
	}
	cache.mutex.Unlock()
	for _, listener := range listeners {
		listener(state)
	}
}

// groupPoolHistory groups the records (see zfs.GroupPoolHistory), and finds the group of each record.
func groupPoolHistory(entries []zfs.PoolHistoryEntry) *PoolHistory {
	if entries == nil {
		entries = []zfs.PoolHistoryEntry{}
	}
	groups := zfs.GroupPoolHistory(entries)
	groupOf := make(map[*zfs.PoolHistoryEntry]*zfs.PoolHistoryGroup, len(entries))
	for _, group := range groups {
		for _, entry := range group.Entries {
			groupOf[entry] = group
		}
	}
	return &PoolHistory{Entries: entries, Groups: groups, GroupOf: groupOf}
}
