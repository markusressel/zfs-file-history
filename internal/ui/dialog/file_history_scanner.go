package dialog

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"time"
	"zfs-file-history/internal/data"
	"zfs-file-history/internal/data/diff_state"
	"zfs-file-history/internal/logging"
	"zfs-file-history/internal/zfs"
)

type fileMeta struct {
	exists  bool
	isDir   bool
	size    int64
	mode    os.FileMode
	modTime time.Time
}

type prefetchResult struct {
	snapPath string
	meta     fileMeta
}

type historyScanner struct {
	filePath              string
	cachedEntries         []*data.SnapshotBrowserEntry
	preComputedDiffStates map[string]diff_state.DiffState
	metaCache             map[string]fileMeta
	workingCopyExists     bool
	workingCopyStat       os.FileInfo
}

func newHistoryScanner(filePath string, cachedEntries []*data.SnapshotBrowserEntry) *historyScanner {
	return &historyScanner{
		filePath:              filePath,
		cachedEntries:         cachedEntries,
		preComputedDiffStates: make(map[string]diff_state.DiffState),
		metaCache:             make(map[string]fileMeta),
	}
}

func (s *historyScanner) scan(loadingMsgFunc func(string)) ([]*data.SnapshotBrowserEntry, error) {
	ds, err := zfs.FindHostDataset(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to find host dataset: %w", err)
	}

	var snapshots []*zfs.Snapshot
	if len(s.cachedEntries) > 0 && s.cachedEntries[0].Snapshot != nil && s.cachedEntries[0].Snapshot.ParentDataset != nil && s.cachedEntries[0].Snapshot.ParentDataset.Path == ds.Path {
		for _, entry := range s.cachedEntries {
			if entry != nil && entry.Snapshot != nil {
				snapshots = append(snapshots, entry.Snapshot)
				s.preComputedDiffStates[entry.Snapshot.Name] = entry.DiffState
			}
		}
	} else {
		var err error
		snapshots, err = ds.GetSnapshots()
		if err != nil {
			return nil, fmt.Errorf("failed to get snapshots for dataset %s: %w", ds.Path, err)
		}
	}

	if loadingMsgFunc != nil {
		loadingMsgFunc("Scanning snapshot history for changes...")
	}

	slices.SortFunc(snapshots, func(a, b *zfs.Snapshot) int {
		return a.GetCreationDate().Compare(b.GetCreationDate())
	})

	workingCopyStat, workingCopyErr := os.Lstat(s.filePath)
	s.workingCopyExists = workingCopyErr == nil
	s.workingCopyStat = workingCopyStat

	s.prefetchStats(snapshots)

	var history []*data.SnapshotBrowserEntry
	var prev *zfs.Snapshot = nil

	for _, snap := range snapshots {
		state, err := s.determineDiffStateBetween(snap, prev)
		if err != nil {
			logging.Error("Failed to determine diff state between snapshots: %s", err.Error())
			state = diff_state.Unknown
		}
		if state != diff_state.Equal && state != diff_state.Unknown {
			history = append(history, &data.SnapshotBrowserEntry{
				Snapshot:  snap,
				DiffState: state,
				IsLoading: false,
			})
			prev = snap
		} else if state == diff_state.Equal {
			prev = snap
		}
	}

	slices.Reverse(history)
	return history, nil
}

func (s *historyScanner) prefetchStats(snapshots []*zfs.Snapshot) {
	var pathsToStat []string
	for _, snap := range snapshots {
		snapPath := snap.GetSnapshotPath(s.filePath)
		state, ok := s.preComputedDiffStates[snap.Name]
		if ok && state == diff_state.Equal && s.workingCopyExists {
			continue
		}
		if ok && state == diff_state.Deleted {
			continue
		}
		pathsToStat = append(pathsToStat, snapPath)
	}

	if len(pathsToStat) == 0 {
		return
	}

	resultsChan := make(chan prefetchResult, len(pathsToStat))
	pathsChan := make(chan string, len(pathsToStat))
	for _, p := range pathsToStat {
		pathsChan <- p
	}
	close(pathsChan)

	numWorkers := 64
	if len(pathsToStat) < numWorkers {
		numWorkers = len(pathsToStat)
	}

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range pathsChan {
				stat, err := os.Lstat(path)
				if os.IsNotExist(err) {
					resultsChan <- prefetchResult{snapPath: path, meta: fileMeta{exists: false}}
				} else if err == nil {
					resultsChan <- prefetchResult{
						snapPath: path,
						meta: fileMeta{
							exists:  true,
							isDir:   stat.IsDir(),
							size:    stat.Size(),
							mode:    stat.Mode(),
							modTime: stat.ModTime(),
						},
					}
				} else {
					resultsChan <- prefetchResult{snapPath: path, meta: fileMeta{exists: false}}
				}
			}
		}()
	}

	wg.Wait()
	close(resultsChan)

	for res := range resultsChan {
		s.metaCache[res.snapPath] = res.meta
	}
}

func (s *historyScanner) getSnapshotMeta(snap *zfs.Snapshot) (fileMeta, error) {
	snapPath := snap.GetSnapshotPath(s.filePath)
	if meta, cached := s.metaCache[snapPath]; cached {
		return meta, nil
	}

	if state, ok := s.preComputedDiffStates[snap.Name]; ok && state != diff_state.Unknown {
		if state == diff_state.Equal && s.workingCopyExists {
			meta := fileMeta{
				exists:  true,
				isDir:   s.workingCopyStat.IsDir(),
				size:    s.workingCopyStat.Size(),
				mode:    s.workingCopyStat.Mode(),
				modTime: s.workingCopyStat.ModTime(),
			}
			s.metaCache[snapPath] = meta
			return meta, nil
		}
		if state == diff_state.Deleted {
			meta := fileMeta{exists: false}
			s.metaCache[snapPath] = meta
			return meta, nil
		}
	}

	stat, err := os.Lstat(snapPath)
	if os.IsNotExist(err) {
		meta := fileMeta{exists: false}
		s.metaCache[snapPath] = meta
		return meta, nil
	} else if err != nil {
		return fileMeta{}, err
	}

	meta := fileMeta{
		exists:  true,
		isDir:   stat.IsDir(),
		size:    stat.Size(),
		mode:    stat.Mode(),
		modTime: stat.ModTime(),
	}
	s.metaCache[snapPath] = meta
	return meta, nil
}

func (s *historyScanner) determineDiffStateBetween(snap, prev *zfs.Snapshot) (diff_state.DiffState, error) {
	sMeta, err := s.getSnapshotMeta(snap)
	if err != nil {
		return diff_state.Unknown, err
	}

	if prev == nil {
		if sMeta.exists {
			return diff_state.Added, nil
		}
		return diff_state.Equal, nil
	}

	prevMeta, err := s.getSnapshotMeta(prev)
	if err != nil {
		return diff_state.Unknown, err
	}

	if sMeta.exists && prevMeta.exists {
		if sMeta.isDir != prevMeta.isDir ||
			sMeta.size != prevMeta.size ||
			sMeta.mode != prevMeta.mode ||
			sMeta.modTime != prevMeta.modTime {
			return diff_state.Modified, nil
		}
		return diff_state.Equal, nil
	} else if sMeta.exists {
		return diff_state.Added, nil
	} else if prevMeta.exists {
		return diff_state.Deleted, nil
	}

	return diff_state.Equal, nil
}
