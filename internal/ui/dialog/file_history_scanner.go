package dialog

import (
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
	filePath          string
	cachedEntries     []*data.SnapshotBrowserEntry
	metaCache         map[string]fileMeta
	workingCopyExists bool
	workingCopyStat   os.FileInfo
	// findSnapshots is findSnapshotsOfPath, captured when the scanner is created (it may scan in the background)
	findSnapshots func(path string, cachedEntries []*data.SnapshotBrowserEntry) ([]*zfs.Snapshot, error)
}

func newHistoryScanner(filePath string, cachedEntries []*data.SnapshotBrowserEntry) *historyScanner {
	return &historyScanner{
		filePath:      filePath,
		cachedEntries: cachedEntries,
		metaCache:     make(map[string]fileMeta),
		findSnapshots: findSnapshotsOfPath,
	}
}

func (s *historyScanner) scan(loadingMsgFunc func(string)) ([]*data.SnapshotBrowserEntry, error) {
	snapshots, err := s.findSnapshots(s.filePath, s.cachedEntries)
	if err != nil {
		return nil, err
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

		workingCopyState, err := s.determineDiffStateAgainstWorkingCopy(snap)
		if err != nil {
			logging.Error("Failed to determine diff state against working copy: %s", err.Error())
			workingCopyState = diff_state.Unknown
		}

		if state != diff_state.Equal && state != diff_state.Unknown {
			history = append(history, &data.SnapshotBrowserEntry{
				Snapshot:             snap,
				DiffState:            state,
				WorkingCopyDiffState: workingCopyState,
				IsLoading:            false,
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

func (s *historyScanner) determineDiffStateAgainstWorkingCopy(snap *zfs.Snapshot) (diff_state.DiffState, error) {
	sMeta, err := s.getSnapshotMeta(snap)
	if err != nil {
		return diff_state.Unknown, err
	}

	if sMeta.exists && s.workingCopyExists {
		if sMeta.isDir != s.workingCopyStat.IsDir() ||
			sMeta.size != s.workingCopyStat.Size() ||
			sMeta.mode != s.workingCopyStat.Mode() ||
			sMeta.modTime != s.workingCopyStat.ModTime() {
			return diff_state.Modified, nil
		}
		return diff_state.Equal, nil
	} else if sMeta.exists {
		// File exists in snapshot, but not in working copy -> Deleted in working copy!
		return diff_state.Deleted, nil
	} else if s.workingCopyExists {
		// File does not exist in snapshot, but exists in working copy -> Added in working copy!
		return diff_state.Added, nil
	}

	return diff_state.Equal, nil
}

// sizesOf returns the sizes of the file in the snapshots of the entries (-1 if it is absent), from the metadata read
// during the scan.
func (s *historyScanner) sizesOf(entries []*data.SnapshotBrowserEntry) []int64 {
	sizes := make([]int64, len(entries))
	for i, entry := range entries {
		sizes[i] = -1
		if meta, ok := s.metaCache[entry.Snapshot.GetSnapshotPath(s.filePath)]; ok && meta.exists {
			sizes[i] = meta.size
		}
	}
	return sizes
}
