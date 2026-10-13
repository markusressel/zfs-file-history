# AGENTS.md

Notes for working on this repository: how it is structured, the rules that are easy to get wrong, and the conventions
of the code.

## Build and test

- Go with cgo: building needs the libzfs headers (e.g. `libzfslinux-dev` on Debian/Ubuntu, see the CI workflow).
- `just build`, `just test`, `just coverage`. Or directly: `go build ./...`, `go vet ./...`, `go test ./...`.
- Before committing: `gofmt -l .` is empty, `go vet ./...` and `go test ./...` pass, the touched packages pass with
  `-race`. CI also runs codespell (see `.codespellignore`).
- `TestOverviewKey` in `internal/ui` fails with `-race` (a test writes `state.Current` while the UI runs). Known, not
  caused by other changes.
- Tests never need ZFS or root: everything that runs `zfs`/`zpool` or calls libzfs is replaceable (see Tests). Tests
  that use the pools of the system skip themselves without them.

## Structure

- `cmd/`: the cobra commands. `root.go` starts the UI, `setup.go` installs the sudoers rule for the event watcher
  (`/etc/sudoers.d/zfs-file-history`, `sudo -n zpool events -v -f`), and with `--pool-actions` the rules for the
  pool actions (checked with `visudo` before installing).
- `internal/zfs/`: everything ZFS. libzfs (`github.com/kraudcloud/go-libzfs`) for datasets and snapshots, the `zfs`
  and `zpool` commands (`runCommand`, `runZfs`, `runZpool`) for the rest. Parsers work on the text output
  (`zpool status -p`, `zpool list -Hp`, ...), not JSON (`-j` needs OpenZFS 2.3+). Sample outputs are in `testdata/`.
- `internal/zfs/zfs_evemt_watcher.go`: follows `zpool events` and emits the emitters of `common.go`:
  `DatasetsLoaded` (snapshots created/destroyed, pools imported/exported, also after F5 via `RefreshZfsData`) and
  `PoolsChanged` (state changes, scrub/resilver/trim, vdev changes, `ereport.*`/`resource.*`; throttled to once a
  second).
- `internal/zfs/pool_action.go`: the actions on pools (`zfs.PoolActions`: scrub start/pause/cancel, TRIM
  start/cancel, clear). Pool operations cannot be delegated, so `RunPoolAction` retries with `sudo -n` if zpool is
  denied, and returns a `PoolActionNeedsRootError` if that is not allowed either: `dialog.NewErrorDialogWithRetry`
  then offers to run it once with sudo or to run `setup --pool-actions` (like `NewMissingPermissionsDialog` does
  for dataset permissions). Actions that lose data or make a pool unavailable (destroy, export, upgrade, `clear -F`) are deliberately
  not offered.
- `internal/state/`: the persisted state (`state.Current`): settings, table column layouts, selections, the session.
- `internal/ui/`: the pages and the root.
  - `root.go` creates the UI: the pages (`Main` = files, `Dataset`, `Pool`; keys `1`-`3`), global keys, the session
    (`session.go`: page, focused component by title, dataset, pool).
  - `page_base.go`: `basePage`, shared by the pages: focusable components and their titles, focus cycling (Tab),
    the shortcut map, `newSplit`/`captureSplitDrags`, `isInFront`.
  - One package per component (`file_browser`, `snapshot_browser`, `dataset_browser`, `dataset_info`,
    `pool_browser`, `pool_status`, ...). Components emit typed events (`Events *util.Emitter[Event]`), the page wires
    them together.
  - `pool_activity/`: the operations running on the pools (scrub, resilver, TRIM, device removal, raidz expansion),
    shown below the header of every page. One `Monitor` for the application (loads `zpool status` of all pools,
    every 5 s while something runs, every minute otherwise, and on the zfs emitters), one `Bar` per page (no height
    while nothing runs; a click shows the pool on the pools page). The progress bar is drawn cell by cell with the
    progress, amount and rate written into it (in the text color of the background of each cell), the time left in
    an aligned column behind it.
  - `dialog/`: dialogs, overlays (file and folder history) and the command menu (`:`). Shown with
    `ShowDialogOnPages`.
  - `table/`: `RowSelectionTable[T]`, the table of all lists: sorting, filtering (`/`), column selection (F2),
    horizontal scrolling, multi-selection, persisted column layouts, `ContentSize`.
  - `util/` (imported as `uiutil`): `DataLoader`, `Debouncer`, `SubscribeUI`, `LoadingContainer`, `ResizableSplit`,
    `SelectionMemory`, content size helpers, formatting.
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
  `shortcutBar.MouseHandler`).
- **tview hands mouse events to all pages until one consumes them** (rivo/tview#926). Dialogs consume clicks next to
  them; things on a page that react to the mouse check `isInFront` (splits, the shortcut bar), so they do nothing
  while a dialog is in front.
- **libzfs handles are shared and not thread safe.** Reads of their property maps hold `golibzfs.Global.Mtx` (see
  `findSnapshotHandles`); nothing inside may call a golibzfs function that locks it itself.
- **Selection changes are debounced** (`uiutil.NewDebouncer(app, uiutil.SelectionDebounceDelay)`) before expensive
  work for the selected entry, so holding an arrow key does not load every entry passed.

## Layout

- Panes are split with `uiutil.ResizableSplit` (via `basePage.newSplit`, registered with `captureSplitDrags`). The
  boundary can be dragged; double-clicking it goes back to the automatic size.
- `SetAutoSize` sizes the first pane by its content on every draw (e.g. the dataset info and the pool status fit
  their text, capped with `uiutil.FitContent`). A drag overrides it, per direction.
- `SetVerticalFunc` switches between side by side and stacked depending on the available space (the pools page:
  side by side if the pools and the widest of status and devices fit). Each direction keeps its own sizes.
- Components that are sized by their content have `ContentSize() (width, height)`, including their border (0 if
  empty). Sizes that differ between entries only grow (see the dataset page), so boundaries do not jump while moving
  through a list.

## Tables

- Lists use `table.RowSelectionTable[T]` (entries implement `TableRowId()`); dialogs use plain `tview.Table`s.
- Clicks into the empty space below the rows do nothing (`uiutil.IgnoreClicksBelowRows`, for every table); in
  `RowSelectionTable` also clicks to the right of the columns.
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
- New UI elements take their colors from the theme.
- Packages must not import upwards: `uiutil` does not import `internal/zfs`, components do not import pages.

## Tests

- testify (`assert`/`require`). Table tests where several inputs share an assertion.
- Functions that run processes or call libzfs are package variables, replaceable in tests (e.g. `listPools`,
  `loadPoolStatus`, `loadDatasetPermissions`; commented "replaceable in tests"). Replace them with `t.Cleanup` to
  restore.
- UI tests run a real `tview.Application` on a `tcell.NewSimulationScreen("UTF-8")`, and read or change widgets only
  via `testutil.OnUiThread`. Wait for background loads with `require.Eventually`. `testutil.StripTags` removes style
  tags from text.
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
