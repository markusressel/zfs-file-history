<h1 align="center">zfs-file-history</h1>
<h4 align="center">Terminal UI for inspecting and restoring file history on ZFS snapshots.</h4>

<div align="center">

[![Programming Language](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)]()
[![Latest Release](https://img.shields.io/github/release/markusressel/zfs-file-history.svg)](https://github.com/markusressel/zfs-file-history/releases)
[![License](https://img.shields.io/badge/license-AGPLv3-blue.svg)](/LICENSE)

![Demo of zfs-file-history](./screenshots/demo.gif)

</div>

# Features

* [x] Terminal based file browser for your datasets
* [x] File history
  * [x] Find the snapshots in which a file was changed
  * [x] Diff against the previous version or your local file
  * [x] Restore files and folders from any snapshot
* [x] Folder history, including size and item count over time
* [x] Snapshot management (create, clone, destroy, hold)
* [x] Dataset overview
  * [x] Tree view
  * [x] Space usage breakdown
  * [x] Edit properties
  * [x] Manage delegated permissions
* [x] Pool health and `zpool status`
* [x] Progress of running scrubs, resilvers, TRIMs and other pool operations on every page
* [x] Start, pause and cancel scrubs and TRIMs, clear pool errors
* [x] Live updates using zpool events
* [x] Filter, sort and configure the columns of all lists
* [x] Remembers your last session

All important shortcuts are shown at the bottom of the screen, press `:` to see everything else.

# How to use

```shell
# Open the current directory
zfs-file-history
# Open a specific directory
zfs-file-history /path/to/folder
```

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
a sudoers rule that lets your user run `zpool events` as root without requiring interactive permission approval.

```shell
sudo zfs-file-history setup
```

### Pool Actions

Scrubs, TRIMs and clearing the errors of a pool (`Enter` on the pools page) need root as well, ZFS cannot delegate
them. zfs-file-history offers to run them once with sudo, or to allow them for your user, which is the same as adding
`--pool-actions` to the setup. Destroying, exporting or upgrading pools is not offered by zfs-file-history at all.

```shell
sudo zfs-file-history setup --pool-actions
```

### Permissions

If a ZFS permission (delegation) is missing, zfs-file-history automatically gives you the option to add the missing permission to the relevant dataset for your user.

However, if you want to give these permissions in one swoop you can also run this one line:

```shell
sudo zfs allow $USER mount,snapshot,destroy,hold,release <dataset>
```

## Configuration

> **Note:**
> The configuration is optional. It contains display and behavior settings of the file browser,
> as well as debugging settings.

Then configure zfs-file-history by creating a YAML configuration file in **one** of the following locations:

* `/etc/zfs-file-history/zfs-file-history.yaml` (recommended)
* `/home/<user>/.config/zfs-file-history/zfs-file-history.yaml`
* `./zfs-file-history.yaml`

```shell
mkdir -p ~/.config/zfs-file-history
nano ~/.config/zfs-file-history/zfs-file-history.yaml
```

### Example

An example configuration file including more detailed documentation can be found
in [zfs-file-history.yaml](/zfs-file-history.yaml).

## State

Besides the configuration file, which is only ever written by you, zfs-file-history remembers some UI settings
(like the columns and sort order of the tables) and your last session in a state file:

* `$XDG_STATE_HOME/zfs-file-history/state.json`, by default `~/.local/state/zfs-file-history/state.json`

Deleting it resets all remembered settings. If you don't want to continue where you left off, start with `--fresh`.

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
