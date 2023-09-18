<h1 align="center">zfs-file-history</h1>
<h4 align="center">Terminal UI for inspecting and restoring file history on ZFS snapshots.</h4>

<div align="center">

[![Programming Language](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)]()
[![Latest Release](https://img.shields.io/github/release/markusressel/zfs-file-history.svg)](https://github.com/markusressel/zfs-file-history/releases)
[![License](https://img.shields.io/badge/license-AGPLv3-blue.svg)](/LICENSE)

<a href="https://asciinema.org/a/HUjG6sJCUfOp2G8b8yXjfCyc9" target="_blank"><img src="https://asciinema.org/a/HUjG6sJCUfOp2G8b8yXjfCyc9.svg" /></a>

</div>

# Features

* [x] Full fledged File Explorer
* [x] Intuitive navigation using arrow keys (and vim bindings)
* [x] Colorful visualization "git diff" style
* [x] Browse through Snapshots and get to the right version quickly
* [x] Restore any file from any snapshot with two keytrokes 

# How to use

## Installation

### Arch Linux ![](https://img.shields.io/badge/Arch_Linux-1793D1?logo=arch-linux&logoColor=white)

**Coming Soon**

```shell
yay -S zfs-file-history-git
```

<details>
<summary>Community Maintained Packages</summary>

None yet

</details>

### Manual

Download the latest release from GitHub:

```shell
# Install dependencies
sudo pacman -S libnotify

curl -L -o zfs-file-history https://github.com/markusressel/zfs-file-history/releases/latest/download/zfs-file-history-linux-amd64
chmod +x zfs-file-history
sudo cp ./zfs-file-history /usr/bin/zfs-file-history
zfs-file-history
```

Or compile yourself:

```shell
git clone https://github.com/markusressel/zfs-file-history.git
cd zfs-file-history
make build
sudo cp ./bin/zfs-file-history /usr/bin/zfs-file-history
sudo chmod ug+x /usr/bin/zfs-file-history
```

## Configuration

Then configure zfs-file-history by creating a YAML configuration file in **one** of the following locations:

* `/etc/zfs-file-history/zfs-file-history.yaml` (recommended)
* `/root/.zfs-file-history/zfs-file-history.yaml`
* `./zfs-file-history.yaml`

```shell
sudo mkdir /etc/zfs-file-history
sudo nano /etc/zfs-file-history/zfs-file-history.yaml
```

### Example

An example configuration file including more detailed documentation can be found
in [zfs-file-history.yaml](/zfs-file-history.yaml).

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
