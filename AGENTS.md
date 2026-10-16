# AGENTS.md

Notes for working on this repository: how it is structured, the rules that are easy to get wrong, and the conventions
of the code.

## Build and test

- Go with cgo: building needs the libzfs headers (e.g. `libzfslinux-dev` on Debian/Ubuntu, see the CI workflow).
- `just build`, `just test`, `just coverage`. Or directly: `go build ./...`, `go vet ./...`, `go test ./...`.
- `just demo [<storyboard>]` records a storyboard of the demo on this system and renders it as a GIF: `overview` (the
  default) walks through the whole application for the README (`screenshots/demo.gif`), the others show one feature
  each for release notes (`screenshots/demo-<name>.gif`; `just demo-list`). The storyboards are in
  `scripts/demo/storyboards.py` (each with its terminal size; smaller ones read better in release notes), their steps
  in `steps.py`; `scripts/demo/driver.py` types the keys while asciinema records (`bin/demo-<name>.cast`), agg renders
  it at twice the font size, `render.py` (ffmpeg, 50 fps) zooms in (`Zoom`: around a fixed point, on a logarithmic
  scale, so it neither pans nor rushes), draws the keys typed in the bottom right corner (marked in the recording by
  the driver; `Key.typed` for text, shown as one word) and scales it to half. A storyboard may wait up to 5 minutes until a scrub (or another operation on a pool) runs, so the activity
  panel is shown: start one after `just demo` (`--without-activity` does not wait, unless the storyboard is about it,
  `needs_activity`). It builds with the build cache (`build ""`, `just build` rebuilds all packages with `-a`).
  The driver runs in a Python environment managed by uv (`scripts/demo/pyproject.toml`, `uv.lock`). It reads the
  screen with pyte: each key other than moving around says what it shows, hides or is typed into (`Key.shows`,
  `hides`, `into`), checked before and after the key (`check_script` refuses a demo without), so a slow load stops
  the demo instead of sending the keys into something else. `Click` clicks what a pattern finds on the screen (hovered
  first, SGR mouse events), e.g. the page tabs, and must say what it shows or hides as well. `Search` types into a
  search field until a pattern shows (e.g. one entry left). `Select` moves the selection with ↓ until the line a pattern finds is selected
  (drawn on the background of the selection, `SELECTED_BACKGROUND`, checked against the theme), for an entry whose
  position differs between systems (e.g. "Enable features" in the actions of a pool). `Ready` marks the recording once the application stopped
  drawing after the start, and `prepare_cast.py` moves everything before it to time 0, so the GIF starts with the
  loaded application. The application runs with `--fresh` and an empty `XDG_STATE_HOME`, so every run is the same
  and the state of the user is not changed. Storyboards wait for what the application shows (e.g. the page tabs,
  `FILES_PAGE`), not for names of datasets or pools. `just demo-test` tests them against fake applications (and that
  every storyboard is safe to run) and runs ruff.
- Before committing: `gofmt -l .` is empty, `go vet ./...` and `go test ./...` pass, the touched packages pass with
  `-race`. `just check` runs what CI runs (gofmt, vet, all tests with `-race`: `just test-race`, `just demo-test`),
  except codespell (see `.codespellignore`).
- Tests never need ZFS or root: everything that runs `zfs`/`zpool` or calls libzfs is replaceable (see Tests). They
  never use the ZFS of the system either, so they behave the same on every machine (see Tests).

## Structure

- `cmd/`: the cobra commands. `root.go` starts the UI, `setup.go` installs the sudoers rules of `zfs.SudoersRules`
  (`/etc/sudoers.d/zfs-file-history`, checked with `visudo` before installing): `--events` (`zpool events -v -f`,
  the event watcher), `--data-errors` (`zpool status -p -v`), `--history` (`zpool history -il <pool>`; the rule of
  older versions, all pools, still works: `SudoersRule.fallback`, rewritten by the next setup),
  `--smart` (`smartctl -j -i -H -A -n standby -- /dev/*`; `--` keeps other options out, smartctl takes one device;
  only offered if smartctl is installed, `SudoersRule.IsAvailable`), `--pool-actions`. Without flags it asks for each rule.
  It adds to the file (`zfs.SudoersFile`, which also reads the files of older versions), so it can run again.
- `internal/zfs/sudoers.go`: the rules, and which ones the user has (`AllowedSudoersRules`: parses `sudo -n -l`,
  which is not logged and needs no password once any rule has NOPASSWD; `sudo -l <command>` cannot be used, it
  also succeeds for commands that need a password). The application offers the missing rules in
  `dialog.NewSetupPermissionsDialog` (at the start if a read-only one is missing, unless "Don't ask again"; command
  menu; `s` in the damaged files dialog), which runs `setup` with sudo in the terminal and emits `zfs.SudoersChanged`:
  the event watcher starts again, the damaged files are reloaded.
- `internal/zfs/`: everything ZFS. libzfs (`github.com/kraudcloud/go-libzfs`) for datasets and snapshots, the `zfs`
  and `zpool` commands (`runCommand`, `runZfs`, `runZpool`) for the rest. Parsers work on the text output
  (`zpool status -p`, `zpool list -Hp`, ...), not JSON (`-j` needs OpenZFS 2.3+). Sample outputs are in `testdata/`.
- `internal/zfs/backend.go`: the functions of the package check their arguments and explain errors (missing
  permissions), then call the `Backend`, which only does what is asked and returns Go types. `System`
  (`system.go`, the libzfs handles in `system_libzfs.go`) is the backend of the system: the only code that runs
  `zfs`, `zpool` or `sudo`, or calls libzfs (`TestOnlySystemRunsCommands`). `Dataset` and `Snapshot` read their
  properties through it (a dataset all at once with the first getter, snapshots with `GetSnapshots`); the snapshot
  names come from the `.zfs/snapshot` folder. `SetBackend` replaces the backend (e.g. in tests) before anything
  runs. `.zfs` itself is never listed as a folder, also with `snapdir=visible` (`util.IsZfsControlDirectory`).
- `internal/zfs/zfs_evemt_watcher.go`: follows `zpool events` and emits the emitters of `common.go`:
  `DatasetsLoaded` (snapshots created/destroyed, pools imported/exported, also after F5 via `RefreshZfsData`) and
  `PoolsChanged` (state changes, scrub/resilver/trim, vdev changes, `ereport.*`/`resource.*`; throttled to once a
  second), and `PoolHistoryRecorded` with each record ZFS logged in the history of a pool (`history_event`, parsed
  into a `PoolHistoryEntry` like `zpool history -il` lists it; only the events ZFS logs itself, not the commands).
  Emitters are safe for concurrent use (the UI subscribes while the watcher emits).
- `internal/zfs/pool_history.go`: the history of a pool (`zpool history -il <pool>`, `PoolHistory`), which only root
  may read: it runs with `sudo -n` (rule `--history`) unless the application runs as root; with the rule of older
  versions all pools, read up to the end of the pool. It can be millions of records (hundreds of MB, zpool takes tens
  of seconds), so it is parsed while zpool prints it (`streamCommand`, reporting its progress), without regular
  expressions, and only read on demand. Records of ioctls are
  skipped: they repeat the command logged next to them, and dump their arguments as they are (binary data, e.g. the
  key of `zfs load-key`). `pool_history_groups.go` groups the records (`GroupPoolHistory`, each record in exactly one
  group): scrubs, resilvers and TRIMs from their start to their end (by the events ZFS logs for them; the commands that
  cancel them are logged after the end), a command with the events ZFS logged for it (logged before it, while it ran,
  and between the records of other commands: matched by dataset, not by position), and the commands one user ran less
  than a minute apart (e.g. a backup tool). Groups may overlap in time. `pool_history.PoolHistoryDialog` (`internal/ui/pool_history`) lists the groups
  newest first in a virtual table (`tview.TableContent`), expanded like the dataset tree (Enter, `+`, `-`, `*`), or
  with `b` each record in the order they were logged, with the events ZFS logged itself (`i`); sorted on the header
  row and its columns chosen with F2 like a `RowSelectionTable` (sorted once, not on each change of the filter;
  `table.SaveColumnLayout`); a filter in both. `pool_history.PoolHistoryCache` (one for the application) keeps the history
  read, grouped: shown again, the dialog shows it at once while it is read again (with the progress in the hint), and
  replaces it if it changed, keeping the selection and the expanded groups (by `poolHistoryEntryKey`). A read goes on
  when the dialog is closed. The events ZFS logs meanwhile are added as they come (`PoolHistoryCache.Record`, from
  `zfs.PoolHistoryRecorded`; batched, grouped again in the background; once if they also are in a read), the
  commands only with the next read. New rows come in above: the selected row stays on its line, unless the list was
  at its top, where the new ones are shown. `h` on the pools page (the pool) and the datasets page (the dataset with its children and snapshots), only offered
  while the rule is allowed (`SetOnShowHistory`, updated on `zfs.SudoersChanged`).
- `internal/zfs/pool_action.go`: the actions on pools (`zfs.PoolActions`: scrub start/pause/cancel, TRIM
  start/cancel, clear, upgrade). Pool operations cannot be delegated, so `RunPoolAction` retries with `sudo -n` if zpool is
  denied, and returns a `PoolActionNeedsRootError` if that is not allowed either: `dialog.NewErrorDialogWithRetry`
  then offers to run it once with sudo or to run `setup --pool-actions` (like `NewMissingPermissionsDialog` does
  for dataset permissions). Actions that make a pool unavailable or may lose data (export, `clear -F`) are
  deliberately not offered. Destroying a pool (`pool_destroy.go`) works like destroying a dataset: a preview
  (`PreviewDestroyPool`: all its datasets, refused if the running system is mounted from it, `protectedMountPaths`),
  a checkbox (`dialog.NewDestroyPoolDialog`), its name typed, and `DestroyPool` refuses with
  `ErrChangedSinceConfirmation` if its datasets changed. Never `-f`: a dataset in use stops it before anything is
  destroyed. `PoolActionDestroy` is not in `PoolActions`, so the sudoers rule never allows it (run once with sudo, or
  copy the command). Upgrade cannot be undone: only offered while `zpool status` suggests it
  (`PoolStatus.CanUpgrade`), red, and confirmed by typing the name of the pool (`dialog.NewPoolUpgradeDialog`, not
  run by the action dialog itself: `dialog.PoolActionOf`). The sudoers rule of former versions without upgrade is
  its `fallback`. `pool_features.go`: single features (`zfs.ListPoolFeatures`: `zpool upgrade -v` and `zpool get`,
  no root) can be chosen in `dialog.PoolFeaturesDialog` (a `RowSelectionTable`, grouped by whether they can be
  enabled and why not) and enabled once confirmed by name
  (`zfs.EnablePoolFeatures`, one `zpool set feature@…=enabled` per feature). Features the property `compatibility`
  of the pool does not allow (its files in `compatibility.d`, e.g. `grub2`; `zpool set` ignores it, `upgrade` does
  not), the loaded kernel module does not support (`/sys/module/zfs/features.pool`, if it lists them; the tools may
  be newer, the dialog says so with the versions of `zfs version`) and enabled ones are grayed out with the reason
  and cannot be checked. Features of the pool the installed ZFS does not know (`unsupported@<guid>`) are listed last. Not part of the sudoers rule (a
  wildcard for the name would also match spaces, so any property could be set): `PoolActionNeedsRootError`
  `AllowedBySetup` is false, and the root dialog only offers to run them once with sudo or to copy the commands.
- `internal/zfs/dataset_actions.go`: the actions on datasets that lose data (destroying a dataset, rolling back) work
  in two steps: a preview lists what would be lost without changing anything (dry run, newer snapshots, holds,
  clones, permissions), the action takes the confirmed preview, checks everything again and refuses with
  `ErrChangedSinceConfirmation` if anything differs. `-R` (destroys clones elsewhere) is never used. Any dataset can
  be destroyed but the root dataset of a pool, and none that is (or has a child) mounted where the running system
  is (`protectedMountPaths`: zfs would destroy the others first and then fail on the busy one); with snapshots or
  child datasets only after a checkbox. The confirmation lists the datasets before the snapshots. The previews of
  destroying (datasets, pools, snapshots) and rolling back run in a `dialog.PreviewDialog` (red border, spinner,
  what is checked, Cancel; `dialog.RunPreview` runs the check and shows the confirmation or the error), shown at once in place of the confirmation, not with a spinner in the action dialog: that
  looked like it was destroyed without a confirmation. Both are confirmed by typing the full name
  (`dialog.NewConfirmByNameDialog`, no button confirms), and have no shortcut key (action dialog and command menu
  only). Tests never run them: `zfs` tests use a fake `Backend` whose missing methods panic,
  UI tests replace the package variables.
- `internal/zfs/mount.go`: mounting and unmounting datasets, loading and unloading their encryption keys
  (`MountAction`; in the action dialog of the datasets page, offered by the state of the dataset: `CanMount`,
  `KeyStatus`, `EncryptionRoot`, `KeyLocation` of `DatasetListEntry`). Nothing is lost, but they work in two steps like
  the actions above: `PlanMountAction` says what is done (unmounting takes everything mounted below the dataset along,
  deepest first, and is refused where the running system is mounted, `protectedMountPaths`; unloading a key unmounts
  all datasets encrypted with it first), `RunMountPlan` refuses with `ErrMountsChangedSinceConfirmation` if that
  changed. Unmounting and unloading a key ask first (`dialog.NewUnmountDialog`). Never `-f`. Linux only lets root
  mount: `System.RunDatasetCommand` retries with `sudo -n`, then returns a `DatasetCommandNeedsRootError`
  (`dialog.NewDatasetCommandRootDialog`: run once with sudo, or copy the commands; there is no sudoers rule, it would
  let the user mount any dataset over the directories of the system). A key with `keylocation=prompt` is loaded in
  the terminal (`uiutil.RunInTerminal`, with sudo unless the user is root or has `load-key` delegated), where zfs
  asks for the passphrase: the application never sees it.
- `internal/disk/`: the health of the disks of the pools, from sysfs (no root, no processes): `Resolve` maps a device
  of `zpool status` to its disks (partitions, device mapper, md), `ReadTemperature` reads hwmon (NVMe, SATA with the
  kernel module drivetemp). Reading the temperature may reset the spin down timer of hard disks, so `Sampler` only
  reads hard disks while their I/O statistics (`/sys/block/<disk>/stat`, which never touch the disk) change, or when
  asked to (a click on the temperature, `t`). The devices pane of the pools page and the disks page sample only while
  they are drawn (`disk_browser.HealthReader`, shared by both: `ReadIfDue` in their `Draw`, every 10 s), so nothing
  is read while another page is shown. The `Monitor` samples after each
  load of the statuses with the same `Sampler`, for the alert of disks at or above their critical temperature
  (`zfs.PoolAlertTemperature`); hard disks count as busy if their statistics changed within 90 s, longer than its
  idle interval. Tests use a fake `/dev` and `/sys`
  (`devDir`, `sysDir`), UI tests the fakes of `internal/disk/disktest` (`newTemperatureSampler`).
  `SmartReader` (`smart.go`) runs `sudo -n smartctl` (rule `--smart`), parses its JSON (ATA, NVMe, SCSI) into
  `SmartReport`s with `Problems` (failed health check, failing or important ATA attributes, NVMe critical warnings,
  media errors and wear) and how close each attribute is to failing (`SmartAttribute.Risk`: ATA over the last
  `ataRiskWindow` points of its normalized value above its threshold, not from the value of a new disk, which smartctl
  does not tell and healthy values settle far below; NVMe spare and endurance; a bar in the `Risk` column of the
  attributes), and keeps them: sudo logs every call. `-n standby` skips sleeping disks (their last report
  is kept). The `Monitor` reads all disks every hour (alert `zfs.PoolAlertSmart`), the devices pane and the disks page
  every 10 minutes while drawn, `s` now; one `SmartReader` for all (`newSmartSource` in `root.go`). The disks page
  (`disk_page.go`, `internal/ui/disk_browser`) lists the disks of all pools (from the statuses of the `Monitor`),
  each disk once (also with a partition for each of several pools), grouped by hard disk or SSD (`Rotational`, from
  sysfs) or by pool, with the values that mean the same for all kinds of disks (`SmartCounters`: bad sectors, wear,
  bytes written, ...; -1 if not reported, units of vendor attributes only where known), the details and attributes
  of the selected one below (what the disk is next to its health, or below it where both do not fit, wrapped where
  still too wide: `InfoText.WrappedLines`; at most `detailsMaxPercent` of the height, so attributes are shown; a click on the name of a pool there shows it on the pools page); Enter or a click on the SMART column, the name of a device or a temperature that is read anyway (only the one of an idle hard disk is read by a click) of the devices pane,
  and a click on a temperature or SMART alert, show the disk there. Fixtures in `internal/disk/testdata` (the permission denied, ATA hot and healthy NVMe ones are from
  real systems).
- `internal/state/`: the persisted state (`state.Current`): settings, table column layouts, selections, the session.
- `internal/ui/`: the pages and the root.
  - `root.go` creates the UI: the pages (`Main` = files, `Dataset`, `Pool`, `Disks`; keys `1`-`4`), global keys, the session
    (`session.go`: page, focused component by title, dataset, pool).
  - `page_base.go`: `basePage`, shared by the pages: focusable components and their titles, focus cycling (Tab),
    the shortcut map, `newSplit`/`captureSplitDrags`, `isInFront`.
  - One package per component (`file_browser`, `snapshot_browser`, `dataset_browser`, `dataset_info`,
    `pool_browser`, `pool_status`, `disk_browser`, ...). Components emit typed events (`Events *util.Emitter[Event]`), the page wires
    them together.
  - `file_browser/`: the files of a folder compared with the selected snapshot. With `a` (or a click on the count in
    the footer, saved in the state) it also lists the entries that exist neither now nor in the selected snapshot,
    but in other snapshots (`FileBrowserEntry.OnlyInSnapshots`, their version in the newest one, which a restore
    takes). They come from the comparison the snapshot browser makes of the folder in all its snapshots
    (`folder_listing.CompareSnapshots`, `FolderChangesLoaded.OnlyInSnapshots`), so nothing is read twice.
  - `pool_activity/`: the operations running on the pools (scrub, resilver, TRIM, device removal, raidz expansion),
    shown below the header of every page. One `Monitor` for the application (loads `zpool status` of all pools,
    every 5 s while something runs, every minute otherwise, and on the zfs emitters; the pool status pane takes the
    progress of a scrub from these loads, see `OnStatusesLoaded`), one `Bar` per page (a panel with
    its own border and background colors, no height while nothing runs; a click shows the pool on the pools page). The progress bar fills the line (1 column of padding)
    and is drawn cell by cell: the filled part in the gradient `theme.Colors.Activity.BarFilled` by column, ending
    with a partly filled cell in eighths (`▏`…`▉`), the progress, amount and rate centered in it (padded to fixed
    widths, so the text does not move; dark or light text by the brightness of each cell), the time left in an
    aligned column behind it, always the same width, so the bar never changes its width (`time_left.go`: the two
    largest units, `⧗    2h 10m`, `⧗    1d 07h`; the ETA, `⚑     14:32` (today, else with the weekday); or both (the
    default), `Monitor.TimeLeftMode`, cycled by a
    click on it or the command menu, saved in the state; placeholders like `⧗   --h --m` until it is known; each a field on its own background,
    `RemainingBackground` and `EtaBackground`, so a switch is noticed). Only the hovered part of a line is
    highlighted (see `clickable.Regions`): the label for the pool, the fields of the time left for switching it. zpool prints
    the time left in several formats, `zfs.PoolActivity.Remaining` is parsed into a `time.Duration`. zpool does not estimate the time left of a TRIM,
    `PoolStatus.TrimRemaining` does, from the start time and progress of each device.
    The same package has the `AlertBar` (above the activities): the problems of the pools (`PoolStatus.Alerts`: a
    state other than ONLINE, data errors, unusable devices or spares, device errors; and disks at their critical
    temperature or with SMART problems, see `internal/disk`), most severe first, from the
    loads of the `Monitor`. It comes up by itself when something is wrong. A failed load keeps the last alerts.
    Both bars share `linePanel` (height, mouse).
    The `Monitor` also loads the damaged files (`zfs.ListDataErrors`: `zpool status -p -v`, retried with `sudo -n`,
    allowed by the rule of `setup`) into a `zfs.DataErrorIndex`, only when the data errors of the pools changed or on
    `RefreshDataErrors` (F5, snapshot changes), not on every load (sudo logs every call). The file and snapshot
    browsers mark them (`SetDataErrors`), `dialog.DataErrorsDialog` lists them (click on the alert, command menu).
    Objects ZFS has no path for (`ds:<0x…>`) can be searched in the snapshots by their inode number (= object
    number; `zfs.FindObjects`, `f` in the dialog): it reads the inodes from `getdents64`, no `lstat` per file
    (millions of files per snapshot), newest snapshot first. The dialog also says how to enable `head_errlog`, but
    does not run it (one-way; the features of the pool action dialog can).
  - `file_history/`: the overlays of the history of a file and of a folder. The file history shows the changes of
    each version (there is no separate diff), `e` in the external diff viewer (`configuration` `Diff`,
    `external_diff_viewer.go`; the paths are arguments, never part of a shell script).
  - `dialog/`: the dialogs and the command menu (`:`), and what the overlays of other packages share
    (`NewOverlayFrame`, `ShowColumnDialogOnPages`). `ShowDialogOnPages` finds what a dialog handles itself by type
    assertions (`PagesAware`, `MouseCapturer`): dialogs of other packages assert that they implement them
    (`var _ dialog.MouseCapturer = ...`), a method that does not match would be skipped silently. Shown with
    `ShowDialogOnPages`; components embed `dialog.Host` (`SetDialogPages`, `ShowDialog`,
    `dialog.ShowColumnSelection` for F2, `RunPreview`), which shows them on the pages of the application, set by the
    page, and on the own pages of the component until then. `NewSelectionDialogWithChecklist` adds a checklist above the options (its own table,
    ↑/↓ move between both, Space toggles); the handler reads it with `Checked`, a copy taken when the action started. The
    description of a selection or text input dialog scrolls when the dialog does not fit, so the checklist, the
    options and the input field are always shown.
  - `table/`: `RowSelectionTable[T]`, the table of all lists: sorting, filtering (`/`), column selection (F2),
    grouping (`SetGroupings`, `b` cycles them: a row above each group that cannot be selected, its title drawn over
    the row so it does not widen the first column; saved with the column layout),
    horizontal scrolling, multi-selection, persisted column layouts, `ContentSize`.
  - `util/` (imported as `uiutil`): `DataLoader`, `Debouncer`, `SubscribeUI`, `LoadingContainer`, `ResizableSplit`,
    `SelectionMemory`, content size helpers, formatting.
  - `clickable/`: `Regions`, the parts of a primitive that are highlighted while hovered and run something when
    clicked. Below `uiutil`, which imports `shortcut_helper`.
  - `shortcut_helper/`: shortcut entries, key names, the shortcut bar at the bottom.
  - `theme/`: all colors (`theme.Colors`), defined in `common.go`, set in `dark.go`. No hard-coded colors for new
    UI elements.

## tview rules

tview is not thread safe, and it redraws the whole screen. Most UI bugs in this project broke one of these:

- **UI thread only.** Widgets are only touched on the UI thread: in input/mouse handlers, draw functions, or
  `application.QueueUpdateDraw`. Comments say "Must be called on the UI thread" / "Only accessed on the UI thread";
  keep them accurate.
- **Never block the UI thread.** Anything that calls libzfs or runs a process runs in the background (the functions
  in `internal/zfs` say so in their comment). Use `uiutil.DataLoader` (`Load`, `LoadQuietly` keeps the old content
  while loading; results of outdated loads are dropped) and deliver results with `QueueUpdateDraw`.
- **No `QueueUpdateDraw` (or `QueueEvent`) that waits on the UI thread from the UI thread.** It deadlocks (or blocks
  while the queue is full). From the UI thread, call directly, or queue from a goroutine.
- **Throttle redraws.** Every `QueueUpdateDraw` redraws everything. Work that produces many updates (loading
  permissions, dragging a split) batches them, e.g. `permissionRedrawInterval`, `splitResizeInterval`. A throttled
  update must not lose the last value (see `trailingResize` in `ResizableSplit`).
- **Emitters from the background:** subscribe with `uiutil.SubscribeUI(emitter, app, callback)`, which runs the
  callback on the UI thread.
- **Mouse capture on the top-level layout.** Drags are handled in the mouse capture of the page (or dialog) layout,
  not of the dragged widget, so a drag is not lost when the mouse leaves it (`captureSplitDrags`). A primitive that
  needs to know when the mouse leaves it returns itself as the capturing primitive while the mouse is on it (see
  `clickable.Regions.HandleMouse`).
- **Text selection** (`uiutil.TextSelection`, installed in `root.go`) has the mouse capture and the after draw
  function of the application: a drag in a pane selects its text (clipped to the inner rect of the deepest primitive
  at the start, found through the visible pages and flexes like `isDescendantOf`), a second click on the same cell
  the word; copied on release with OSC 52 (`Screen.SetClipboard`). Down and clicks are passed on, a drag is not.
  Drags that start on a border (split boundaries) are left to the page. tview's double clicks stay disabled
  (`tview.DoubleClickInterval`), they would delay every click.
- **Parts that are hovered and clicked use `clickable.Regions`** (the activity, alert and shortcut bars, the parts
  of a table footer: `uiutil.FooterPart`, e.g. "… unmounted hidden"): `Draw`
  adds each part where it draws it (`Reset`, `Add`, `DrawHover` at the end), the mouse handler calls `HandleMouse`.
  No second layout for hit testing. A region's `Key` identifies it across draws (regions with the same key are
  highlighted together); the last added region wins where they overlap. Regions exist once drawn: UI tests draw
  (`app.ForceDraw()`) before sending mouse events. Links in a text (the name of a pool, the fields of the devices)
  are `clickable.TextLink`s by their position in the text (`AddTextLinks` places them, scrolled and cut off); a
  primitive with regions takes `Regions.WrapMouseHandler`: a click on a region runs it, everything else (pressing,
  which focuses, scrolling) goes on to the primitive, also on a region.
- **tview hands mouse events to all pages until one consumes them** (rivo/tview#926). Dialogs consume clicks next to
  them; things on a page that react to the mouse check `isInFront` (splits, the shortcut bar), so they do nothing
  while a dialog is in front.
- **libzfs handles are shared and not thread safe.** Reads of their property maps hold `golibzfs.Global.Mtx` (see
  `findSnapshotHandles`); nothing inside may call a golibzfs function that locks it itself.
- **Selection changes are debounced** (`uiutil.NewDebouncer(app, uiutil.SelectionDebounceDelay)`) before expensive
  work for the selected entry, so holding an arrow key does not load every entry passed.

## Layout

- Panes there is nothing to do in (the path overview, the details of a disk) are not in `focusableComponents`, and a
  click does not focus them either (`uiutil.WithoutFocus` in their `MouseHandler`).
- A pane without content (yet) says why in its middle, with `uiutil.DrawEmptyState` (lines wrapped and centered):
  the empty state of a `RowSelectionTable` (`SetPlaceholder`, no matches for the filter) and `uiutil.LoadingView`
  (a spinner of two cells above the message, centered, how far it got below it, `SetDetail`; colors
  `theme.Colors.Layout.Loading`).
- Panes are split with `uiutil.ResizableSplit` (via `basePage.newSplit`, registered with `captureSplitDrags`). The
  boundary can be dragged; a right click on it goes back to the automatic size.
- `SetAutoSize` sizes the first pane by its content on every draw (e.g. the dataset info and the pool status fit
  their text, capped with `uiutil.FitContent`). A drag overrides it, per direction. Where the cap leaves an
  `InfoText` pane narrower than its text, its lines are wrapped at the width it got (`InfoText.Wrap`, again when the
  width changes), not cut off; its `ContentSize` stays the width of the text not wrapped.
- `SetVerticalFunc` switches between side by side and stacked depending on the available space (the pools page:
  side by side if the pools and the widest of status and devices fit). Each direction keeps its own sizes.
- Components that are sized by their content have `ContentSize() (width, height)`, including their border (0 if
  empty). Sizes that differ between entries only grow (see the dataset page), so boundaries do not jump while moving
  through a list.

## Tables

- Lists use `table.RowSelectionTable[T]` (entries implement `TableRowId()`); dialogs use plain `tview.Table`s. Plain
  tables that may have more rows than lines (the command menu, the damaged files, the history of a pool) are placed
  in the layout as `scrollbar.Table`, which shows a scrollbar while not all rows fit, like
  `RowSelectionTable` and `scrollbar.TextView` do.
- The selected row is a dark bar on which each cell keeps its own text color (`theme.SelectedStyle`, lightened where
  it would be hard to read), so colors that mean something (states, a destructive action) stay visible.
  `RowSelectionTable` does this for all cells; plain tables use `uiutil.SetSelectionStyle` and
  `uiutil.KeepColorWhenSelected` for their colored cells.
- Parts of a cell that react to the mouse (e.g. the toggle of a dataset in the tree, `▾`/`▸` in place of its branch
  line, so the names do not move) are `table.CellRegion`s (`SetCellRegionsFunc`): placed where tview drew the column
  (`CellAt`), so the widths and the horizontal scrolling are not computed again; a click runs them instead of
  selecting the row.
- Clicks into the empty space below the rows do nothing (`uiutil.IgnoreClicksBelowRows`, for every table); in
  `RowSelectionTable` also clicks to the right of the columns.
- Lists of switches in dialogs (the checklist of a `SelectionDialog`, the features of a pool, the permissions): a
  click checks or unchecks only the highlighted row, like Space; a click on another row selects it
  (`uiutil.OnClickOnSelectedRow`, `RowSelectionTable.SetClickOnSelectedFunc`), so a stray click never changes one
  by accident.
- The header row: `←`/`→` change the sort column, `Enter` the direction. With the mouse, the first click selects the
  header, the next ones sort by the clicked column or toggle the direction.

## Shortcuts

- Each component returns its entries (`GetShortcutMap`), shown at the bottom for the focused component. Entries with
  `Run` are also commands of the command menu; `OnlyInMenu` entries are not shown at the bottom.
- Key names follow `shortcut_helper/key_names.go` (characters as typed, named keys capitalized, modifiers lowercase:
  `ctrl+f`, `shift+⭾`, `F2`). `TestShortcutStyle` checks all of them in the code, including that they can be pressed.
- The entries at the bottom are clickable: a click sends the key event of the entry (`keyEvent`) to the application,
  as if it was typed, so it reaches the focused component. The clicked key of an entry with several keys (e.g.
  `[1ǀ2ǀ3]`) is pressed, otherwise the first one. The entry below the mouse is highlighted
  (`theme.Colors.ShortcutMap.HoverBackground`).
- Shortcut entries are grouped (`GroupAction`, `GroupView`, `GroupNavigation`, `GroupGlobal`), each group has its key
  color.

## Code style

- Comments are full sentences that explain why, not what. Every exported identifier and most unexported ones have a
  comment starting with their name. Struct fields say which thread may access them.
- Match the surrounding code: naming, receiver names (`poolPage`, `datasetInfo`, ...), file layout.
- Escape text for tview with `tview.Escape`, or use `txwidgets.Span(color, format, args...)`, which escapes its
  arguments itself (do not escape twice).
- Key information in dialog texts (names of datasets, snapshots, pools, sizes) stands out with `dialog.Highlight`, in
  the dialogs that show style tags (`newTaggedSelectionDialog`, `NewConfirmByNameDialog`); what is lost for good with
  `theme.Colors.Dialog.Danger`, and their confirmations have a red border (`SetDanger`).
- New UI elements take their colors from the theme.
- Panes that describe one thing (the dataset info, the details of a disk) are built with `uiutil.InfoText`: a verdict
  line first (whether all is well, the dot in the color of the worst part), then the values in sections, labels dim
  and values bright (`theme.Colors.Info`). Color tells states, not the kind of a value; defaults that say nothing are
  left out; derived values (how full, how much compression saves, years instead of hours) beat raw ones; bars
  (`InfoBar`) where a fill level reads faster. A part of a whole is shown in the whole (`InfoStackedBar`, e.g. a
  dataset in its pool), the values below with a swatch of their color as the legend.
- Packages must not import upwards: `uiutil` does not import `internal/zfs`, components do not import pages.

## Tests

- testify (`assert`/`require`). Table tests where several inputs share an assertion.
- Functions that run processes or call libzfs are package variables, replaceable in tests (e.g. `listPools`,
  `loadPoolStatus`, `loadDatasetPermissions`; commented "replaceable in tests"). Tests change package variables only
  with `testutil.Replace(t, &variable, value)` (`TestPackageVariablesAreReplacedWithReplace`; `TestMain` may set them
  for the whole package): it restores them when the test ends, after the apps of the test stopped, and fails a test
  that runs in parallel (the variables are shared by the tests of a package; packages run in processes of their own).
  Each test has its own state with `statetest.Use` (`state.Current` is nil without it: nothing is saved), and new zfs
  emitters with `zfstest.UseNewEmitters` (the UI of `internal/ui` is created with `newUi`, which does that):
  subscriptions are never removed, an emit reached the UI of every earlier test.
- Test-only code: shared helpers of UI tests in `internal/testutil`; helpers and fakes for the users of one package
  in `<package>/<package>test` (`disktest`, `statetest`, `zfstest`).
- Test binaries use `zfs.NoSystem` (everything fails with `zfs.ErrNoSystem`, like a system without ZFS) unless a test
  sets another backend (`testutil.Replace` on `backend` in `internal/zfs`, `TestMain` elsewhere): with the System, tests saw the pools of each machine (none in CI) and passed on one but not on
  another. The tests of `internal/zfs` test the System itself (with its commands replaced). `internal/ui` sets
  `testSystem` once (loads of earlier tests may still run), which a test gives pools with `setPools`.
- UI tests run a real `tview.Application` created with `testutil.NewApp` (`testutil.App` without its screen; the one of
  `CreateUi` gets its screen with `testutil.UseSimulationScreen`): a simulation screen (80x25 or `WithSize`), the mouse
  enabled and no double clicks, like the application (`TestAppsAreCreatedWithNewApp`; tests that set up their own
  forgot one of them, and their clicks did nothing). Started with `testutil.StartApp` (the root set before the event
  loop) or `testutil.RunApp`,
  which waits until its event loop returned when the test ends (`Stop` alone does not: a handler still running raced
  with the cleanups of the test, `TestAppsAreRunWithRunApp`). They read or change widgets only
  via `testutil.OnUiThread` (which does not draw). Read the screen with `testutil.ScreenText`, which draws first: a
  change is only shown once something draws, and a test that relied on a background load to redraw in time failed on
  slower machines (`TestScreenIsDrawnBeforeRead` checks that readers of the screen draw). Wait for background loads
  with `require.Eventually`, for a state rather than for a time (`testutil.WaitForText`, `WaitForPage`, ...).
  Injected keys and clicks are handled in another queue than `OnUiThread`: `testutil.Press`, `Type`, `Click`,
  `ClickText` and `Drag` wait until they were handled (`testutil.WaitForEvents`; not a sleep, too short on a busy
  machine; `TestClicksUseTestutil`). Updates the handlers queue (e.g. closing a dialog) can still run after that: wait
  for their result, or with `testutil.WaitForUpdates` until the queued ones ran, to check that a key did nothing
  (instead of a sleep). Wait for a load with what it ends with (`IsLoading`, an event), not for a time. Tests that draw a primitive themselves get their screen from `testutil.NewScreen` or
  `DrawToScreen`; the text is read with `testutil.ScreenLines` and `FindInScreen` (a rune per cell, so it lines up with
  the columns), a cell with `screen.Get` (`TestScreenIsReadWithTestutil`). The screen of a running app is only read on the UI thread
  (`ScreenText`, `FindText`, or inside `OnUiThread`): it may redraw meanwhile, and a read saw it half drawn
  (`TestScreenOfRunningAppIsReadOnTheUiThread`). `testutil.StripTags` removes style tags
  from text.
- Regression tests start their comment with `// Regression:` and fail without the fix.
- Fixtures from real systems go into `testdata/`, with serial numbers and other identifying data replaced.

## Commits and pull requests

- Commit messages: a short summary in the imperative, sentence case (e.g. "Add a pools page with the health and
  status of the pools"), then a body that explains why.
- Pull requests: `## Summary` (what and why, grouped by area), `## Notes` (known issues, things not done),
  `## Test plan` (checkboxes; only check what was actually done). Label `enhancement` or `bug`, used by
  release-drafter.
- The README is short on purpose: a feature checklist, usage, setup. Details belong into the application (shortcut
  bar, command menu).
