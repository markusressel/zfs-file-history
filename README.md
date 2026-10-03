<h1 align="center">zfs-file-history</h1>
<h4 align="center">Terminal UI for inspecting and restoring file history on ZFS snapshots.</h4>

<div align="center">

[![Programming Language](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)]()
[![Latest Release](https://img.shields.io/github/release/markusressel/zfs-file-history.svg)](https://github.com/markusressel/zfs-file-history/releases)
[![License](https://img.shields.io/badge/license-AGPLv3-blue.svg)](/LICENSE)

[![asciicast](./screenshots/asciinema.svg)](https://asciinema.org/a/1157784)

</div>

# Features

* 📁 **File browser:** Navigate datasets and snapshot contents in a terminal-based file explorer.
* ⌨️ **Keyboard-first navigation:** Use arrow keys and optional Vim key bindings for efficient traversal.
* 🔍 **Dual Diff Comparison Modes:** Inspect file changes inside the history overlay using two modes:
  * **vs Predecessor:** Chronological comparison showing how the file evolved over snapshot versions (Additions in
    Green, Deletions in Red, baseline labeled as `Initial`).
  * **vs Working Copy:** Direct comparison showing each snapshot version relative to your current local file, with
    context-aware statuses (`Present` to restore, `Absent` to remove, `Modified`, or `Identical`).
* 🩹 **Graceful Diff Fallbacks:** Diff views automatically fall back to `/dev/null` when files are missing on either
  side (e.g. deleted locally or missing in a snapshot), showing clean addition/deletion diffs instead of CLI execution
  errors.
* 📂 **Folder history:** `h` on a folder (or on the header row for the current one) shows a timeline of the snapshots
  in which its content changed (`+3 −1 ~2`), sparklines of its size and number of items over time, and the changed
  entries of each snapshot, compared to the previous snapshot or to now (`d`). Restore single entries or the whole
  folder from a snapshot, or open the history of an entry (`h`).
* 🧭 **Overview:** Below the file browser, the current folder compared to the selected snapshot (`+2 −1 ~4`), a
  sparkline of how many of its entries differ from now in each snapshot (to see how far back you have to go, and
  since when it is unchanged), and one of the size of the selected file (its number of versions and last change).
  The snapshot selected in the snapshot list is highlighted in them. `o` hides it to make room for the files, and
  shows it again (remembered between runs).
* 🕘 **Snapshot version lookup:** Move through snapshots to locate the required file revision. Snapshots in which a
  new version of the selected file or folder starts are bright, the ones holding the same as the previous snapshot
  are dimmed (without a selected entry: the snapshots in which the folder changed, or data was written to the
  dataset). `v` shows only the snapshots with changes, like the histories (remembered per screen). `h` in the snapshot
  list (or its action menu) opens the history of the selected file or folder at that snapshot.
* ↕️ **Column-based sorting:** Sort table entries by any supported column in ascending or descending order.
* 🧱 **Configurable columns:** Select and order the columns of all tables (`F2`): files, snapshots, datasets, the
  file and folder history and the dataset properties.
  The columns and the sort order are remembered between runs (in `~/.local/state/zfs-file-history/state.json`,
  see [State](#state)), `r` in the column dialog resets them.
* 🔎 **Filtering:** Filter files, snapshots and datasets as you type (`ctrl+f`), with glob patterns like `*.txt`.
  The filter is edited like a shell prompt: `ctrl+←`/`ctrl+→` jump and `ctrl+Backspace`/`ctrl+Delete` delete whole
  words, `ctrl+w`/`ctrl+u`/`ctrl+k` work as well.
* 🌳 **Dataset overview:** Browse all datasets as a collapsible tree (`-`/`+`, `*` for all, like htop) or as a flat
  list (`t`), unmounted datasets are hidden by default (`u`). Both choices are remembered between runs. The used
  space is broken down into snapshots, the dataset itself, its children and its refreservation (sortable, to find the
  datasets whose snapshots use the most space), colored by how big it is compared to the other datasets.
* ⚙️ **Dataset properties:** `e` in the dataset overview shows all ZFS properties of a dataset (`zfs get all`) with
  their source, filterable and sortable. Change them (`zfs set`), add user properties (`module:property`, `a`), or
  reset local values to the inherited or default ones and remove user properties (`zfs inherit`); with `sudo` if
  needed.
* 🔑 **ZFS permissions:** The `Perms` column of the dataset overview shows which delegated ZFS permissions
  (`zfs allow`) you have on each dataset, e.g. `sdmh-----` for snapshot, destroy, mount and hold. `p` shows the
  details: who grants each permission and all delegations of the dataset and its parents. There you can also grant
  permissions to yourself or revoke them, for the dataset and its children (`zfs allow` / `zfs unallow`); the changes
  are applied with `sudo` if needed. The dataset info box of the dataset overview lists your permissions as well.
* ♻️ **Point-in-time restore:** Restore a selected file directly from a selected snapshot. Fully supports restoring
  files that are absent in a snapshot by deleting the current working copy copy.
* 🕰️ **Relative times:** `T` switches all times in tables between absolute dates and relative ones like
  "3 minutes ago" (remembered between runs). Recent times count up live.
* ⌨️ **Inline shortcuts:** The available keys are shown at the bottom of each page and overlay, grouped and
  color-coded: actions first, then view options, navigation and global keys. `?` (or `F1`) hides
  them to make room in small terminals, and shows them again (remembered between runs).
* 🖥️ **Responsive layout:** Dialogs and overlays automatically scale and reposition themselves during terminal resizing,
  dynamically clamping to screen bounds to prevent clipping.
* 🗃️ **Snapshot columns per screen:** On the files screen, the snapshot list is about the path you look at: `Size`
  and `Modified` of the selected entry in each snapshot (which version it holds; the number of items for folders), and
  the changes of the folder since
  the previous snapshot (`Changes`, e.g. `+3 −1 ~2`) or compared with now (`vs now`). On the datasets screen, it is
  about the whole dataset: `Used`, `Written`, `Refer`. All columns can be shown on both screens (`F2`), each screen
  remembers its own.
* 📏 **Written:** The `Written` column of the snapshot list shows how much was written between a snapshot and its
  predecessor (`zfs get written`): `0 B` (dimmed) for snapshots in which nothing changed. `Used` and `Written` are
  colored by how big they are compared to the other snapshots (logarithmically, so outliers do not hide the rest).
  Deleting files writes
  nothing; the snapshot right before a big deletion stands out in `Used` instead, as it is the only one still holding
  the deleted data.
* 🗂️ **Snapshot lifecycle actions:** Create, clone and destroy snapshots from within the UI.
* 🔒 **Snapshot holds:** Hold snapshots (`zfs hold`, tag `zfs-file-history`) to protect them from being destroyed,
  e.g. by automatic pruning while you investigate, and release them again. Only holds with this tag are ever
  released, holds of other tools (e.g. replication) stay untouched. Held snapshots have a 🔒 in front of their name,
  the `Holds` column shows the number of holds.

# How to use

## Installation

### Arch Linux ![](https://img.shields.io/badge/Arch_Linux-1793D1?logo=arch-linux&logoColor=white)

```shell
yay -S zfs-file-history-git
```

<details>
<summary>Community Maintained Packages</summary>

None yet

</details>

### Manual

Compile yourself:

```shell
git clone https://github.com/markusressel/zfs-file-history.git
cd zfs-file-history
make deploy
```

## One Time Setup

### Background Event Listening

zfs-file-history can listen to zpool events to automatically update the UI when things change in the background.
Unfortunately, due to the current design of ZFS, this requires root privileges.
To avoid having to run zfs-file-history as root, a one-time setup command can be run as root, which creates
a suders rule that lets your user run `zpool events` as root without requiring interactive permission approval.

```shell
sudo zfs-file-history setup
```

### Permissions

To create, destroy, hold or release ZFS snapshots, the user running zfs-file-history needs to have the appropriate
permissions, f.ex.:

```shell
sudo zfs allow markus mount,snapshot,destroy,hold,release rpool/HOME/default/markus
```

If a permission is missing, zfs-file-history tells you which ones are needed on which dataset, shows the matching
`zfs allow` command and offers to run it for you (it asks for your `sudo` password) or to copy it. Once the
permissions are in effect, the action you tried is repeated automatically (destroying asks for confirmation again),
and the permissions shown in the dataset overview are updated.
Destroying snapshots checks the permissions before asking for confirmation, as the dry run of `zfs destroy`
succeeds without them.

## Configuration

> **Note:**
> The configuration is optional. It contains display and behavior settings of the file browser,
> as well as debugging settings.

Then configure zfs-file-history by creating a YAML configuration file in **one** of the following locations:

* `/etc/zfs-file-history/zfs-file-history.yaml` (recommended)
* `/home/<user>/.config/zfs-file-history/zfs-file-history.yaml`
* `./zfs-file-history.yaml`

```shell
mkdir -P ~/.config/zfs-file-history
nano ~/.config/zfs-file-history/zfs-file-history.yaml
```

### Example

An example configuration file including more detailed documentation can be found
in [zfs-file-history.yaml](/zfs-file-history.yaml).

## State

Besides the configuration file, which is only ever written by you, zfs-file-history remembers some UI settings
(the columns and sort order of the tables, the tree view and hidden unmounted datasets of the dataset overview, the
comparison mode of the file and folder history, relative times, hidden shortcuts and the hidden overview)
in a state file:

* `$XDG_STATE_HOME/zfs-file-history/state.json`, by default `~/.local/state/zfs-file-history/state.json`

It is written by the application and specific to the machine, so there is no need to copy it to other systems.
Deleting it resets all remembered settings.

# Dependencies

See [go.mod](go.mod)

# Similar Projects

* [zfs-snap-diff](https://github.com/j-keck/zfs-snap-diff)
* [snapshot-explorer](https://github.com/atheriel/snapshot-explorer)

# License

```
zfs-file-history
Copyright (C) 2023  Markus Ressel

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
```
