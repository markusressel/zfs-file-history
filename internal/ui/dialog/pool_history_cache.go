package dialog

import (
	"context"
	"sync"
	"time"
	"zfs-file-history/internal/zfs"
)

// PoolHistoryCache keeps the history of the pools read for the PoolHistoryDialog while the application runs. zpool
// takes many seconds to print the history of a large pool (it can be hundreds of MB), so a dialog shown again shows
// the history read before right away, while it is read again in the background (see Watch): ZFS cannot tell what was
// added since. A read goes on when the dialog is closed, so it is done sooner when the dialog is shown again. The
// events ZFS logs while the application runs are added as they come (see Record), the commands with the next read.
type PoolHistoryCache struct {
	// read reads the history of a pool, see zfs.PoolHistory
	read func(ctx context.Context, pool string, progress func(zfs.PoolHistoryProgress)) ([]zfs.PoolHistoryEntry, error)

	mutex sync.Mutex
	// pools are the histories of the pools by name. Guarded by mutex.
	pools map[string]*cachedPoolHistory
	// nextListener is the id of the next listener of Watch. Guarded by mutex.
	nextListener int
	// recordDelay is how long Record collects the records before they are added, see Record
	recordDelay time.Duration
}

// poolHistoryRecordDelay is how long the records reported by ZFS are collected before they are added to the history
// (see PoolHistoryCache.Record): a recursive snapshot or the pruning of old ones logs hundreds at once, and each time
// the history is grouped again.
const poolHistoryRecordDelay = 500 * time.Millisecond

// cachedPoolHistory is the history of a pool in the PoolHistoryCache, and its read in progress. Guarded by the mutex of
// the cache.
type cachedPoolHistory struct {
	// state is what the listeners were told last
	state PoolHistoryState
	// listeners are told about each change of state, by the id Watch returned
	listeners map[int]func(PoolHistoryState)
	// recorded are the records reported by ZFS (see Record) that are not in state.History yet: since the read in
	// progress started, or since they were added last
	recorded []zfs.PoolHistoryEntry
	// adding is set while the records are about to be added, see addRecorded
	adding bool
	// generation counts the histories read, so records added to an older one are dropped, see addRecorded
	generation uint64
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
	return &PoolHistoryCache{read: read, pools: map[string]*cachedPoolHistory{}, recordDelay: poolHistoryRecordDelay}
}

// Record adds a record ZFS logged while the application runs (see zfs.PoolHistoryRecorded) to the history of its pool,
// if it was read. The records are collected for a moment (see poolHistoryRecordDelay), then added, grouped again
// in the background. A record that comes during a read may be in its output or not, so it is added to the history
// read unless it is in it. May be called on any thread.
func (cache *PoolHistoryCache) Record(entry zfs.PoolHistoryEntry) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cached := cache.pools[entry.Pool]
	if cached == nil || (cached.state.History == nil && !cached.state.Reading) {
		// the history of the pool was not read (or could not be): the next read has it
		return
	}
	cached.recorded = append(cached.recorded, entry)
	if cached.state.Reading || cached.adding {
		return
	}
	cached.adding = true
	time.AfterFunc(cache.recordDelay, func() { cache.addRecorded(cached) })
}

// addRecorded adds the records reported by ZFS to the history, see Record. Runs in the background.
func (cache *PoolHistoryCache) addRecorded(cached *cachedPoolHistory) {
	cache.mutex.Lock()
	cached.adding = false
	if cached.state.Reading || cached.state.History == nil || len(cached.recorded) == 0 {
		// a read adds them once it is done
		cache.mutex.Unlock()
		return
	}
	base, recorded, generation := cached.state.History, cached.recorded, cached.generation
	cached.recorded = nil
	cache.mutex.Unlock()

	// Appended without a copy (a pool may have millions of records): only behind the records of base, which no
	// history shown reaches, as the records are only appended to the newest history.
	entries := append(base.Entries, recorded...)
	history := groupPoolHistory(entries)
	cache.update(cached, func(state *PoolHistoryState) bool {
		if cached.generation != generation {
			// read again meanwhile, which has the records
			return false
		}
		cached.generation++
		state.History = history
		if len(cached.recorded) > 0 && !cached.adding {
			cached.adding = true
			time.AfterFunc(cache.recordDelay, func() { cache.addRecorded(cached) })
		}
		return true
	})
}

// withRecorded returns the records read with the ones reported by ZFS since the read started that are not among them,
// at their end. Records of the same second are compared: the time of an event is the time of its record.
func withRecorded(entries []zfs.PoolHistoryEntry, recorded []zfs.PoolHistoryEntry) []zfs.PoolHistoryEntry {
	if len(recorded) == 0 {
		return entries
	}
	earliest := recorded[0].Time.Unix()
	for _, entry := range recorded {
		earliest = min(earliest, entry.Time.Unix())
	}
	read := map[poolHistoryEntryKey]bool{}
	// from the end, where the records of the time they were reported are
	for i := len(entries) - 1; i >= 0 && entries[i].Time.Unix() >= earliest; i-- {
		read[poolHistoryKeyOf(&entries[i])] = true
	}
	for _, entry := range recorded {
		if !read[poolHistoryKeyOf(&entry)] {
			entries = append(entries, entry)
		}
	}
	return entries
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
		// the read has the records reported until now
		cached.recorded = nil
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
		cache.update(cached, func(state *PoolHistoryState) bool {
			state.Progress = progress
			return true
		})
	})
	if err != nil {
		cache.update(cached, func(state *PoolHistoryState) bool {
			state.Reading, state.Err = false, err
			return true
		})
		return
	}
	for {
		// grouped outside the lock: records reported meanwhile are added before the read is done
		cache.mutex.Lock()
		recorded := cached.recorded
		cache.mutex.Unlock()
		history := groupPoolHistory(withRecorded(entries, recorded))
		done := false
		cache.update(cached, func(state *PoolHistoryState) bool {
			if len(cached.recorded) != len(recorded) {
				return false
			}
			cached.recorded, cached.generation = nil, cached.generation+1
			state.Reading, state.Err = false, nil
			state.History, state.Expected = history, state.Progress.Bytes
			done = true
			return true
		})
		if done {
			return
		}
	}
}

// update changes the state of the history of a pool, and tells its listeners, unless change returns false. They are
// called outside the lock, so they may call Watch or stop, in the order of the updates: updates come from the one
// read of the pool, or from adding the records reported by ZFS, which waits for the read.
func (cache *PoolHistoryCache) update(cached *cachedPoolHistory, change func(state *PoolHistoryState) bool) {
	cache.mutex.Lock()
	if !change(&cached.state) {
		cache.mutex.Unlock()
		return
	}
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
