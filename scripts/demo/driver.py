#!/usr/bin/env python3
"""Runs zfs-file-history in a pty and types the keys of the demo, for "just demo-record" (asciinema rec --headless).

The output of the application is passed through, so asciinema records it. The demo shows the files page, the search
and the history of a file, the datasets page, the pools page with its actions, and the command menu. It waits until a
scrub (or another pool operation) runs, so the activity panel is shown (--without-activity does not); the time until
then is not in the GIF, see Ready.

The application starts with --fresh and an empty state (XDG_STATE_HOME is a temporary directory): without the
selections, table layouts and toggles of earlier runs, every run of the demo is the same, and it does not change the
state of the user.

The keys are not typed on a timer alone: a copy of the output is fed into a terminal emulator (pyte), so the driver
knows the screen. A key that opens or closes something says what it shows or hides (Key.shows, Key.hides), a key
typed into something says what must be shown (Key.into, Search.into). The driver checks these before and after each
key, and check_script refuses a demo in which a key other than moving around has none, before the application is
started. If a screen does not show up (a slow load, a dialog that did not open or close), the driver quits the
application instead of typing the following keys into something else, and writes the reason into the error file.
asciinema does not pass the exit code on, so "just demo-record" checks that file.

usage: uv run driver.py [--without-activity] <binary> <start directory> <error file>
"""

from __future__ import annotations

import argparse
import fcntl
import os
import pty
import re
import select
import signal
import struct
import sys
import tempfile
import termios
import time
from dataclasses import dataclass
from typing import BinaryIO

import pyte

from prepare_cast import READY_MARKER

UP, DOWN, RIGHT, LEFT = "\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D"
TAB, ENTER, ESC = "\t", "\r", "\x1b"
CTRL_Q = "\x11"

# keys that only move the selection or the focus, and can be typed without checking the screen
NAVIGATION_KEYS = {UP, DOWN, RIGHT, LEFT, TAB}

# how long a screen may take to show up (or go away) before the demo is stopped
WAIT_TIMEOUT = 15.0
# how long the application may take to quit, after ctrl+q or SIGTERM
QUIT_TIMEOUT = 5.0
# the size of the terminal if the driver does not run in one (asciinema passes its --window-size)
DEFAULT_SIZE = (179, 42)


@dataclass(frozen=True)
class Key:
    """A key of the demo, typed after waiting delay seconds.

    shows: a pattern that must not be on the screen before the key, and must be after it (e.g. a dialog it opens).
    hides: a pattern that must be on the screen before the key, and must not be after it (e.g. a dialog it closes).
    into: a pattern that must be on the screen when the key is typed (e.g. the menu the text is typed into).
    """

    keys: str
    delay: float = 0.0
    shows: str | None = None
    hides: str | None = None
    into: str | None = None


@dataclass(frozen=True)
class Wait:
    """Waits until the pattern is on the screen, e.g. until the application has loaded, at most timeout seconds
    (WAIT_TIMEOUT if None)."""

    pattern: str
    timeout: float | None = None


@dataclass(frozen=True)
class Ready:
    """Waits until the application wrote nothing for quiet seconds (e.g. its layout settled after loading), and marks
    the recording there: prepare_cast drops what was shown before, so the GIF starts with the application ready."""

    quiet: float = 0.5


@dataclass(frozen=True)
class Quit:
    """Quits the application (ctrl+q) after waiting delay seconds, and waits until it has quit."""

    delay: float = 0.0


@dataclass(frozen=True)
class Search:
    """Types text into a search field one character at a time, delay seconds apart, until the pattern until is on the
    screen (e.g. only one entry is left). into is a pattern for the field, e.g. its label: after each character, the
    driver waits until the field shows the text typed so far, so it knows that the character arrived. Stops the demo
    if until is not shown after the whole text."""

    text: str
    until: str
    into: str
    delay: float = 0.0


Step = Key | Wait | Ready | Search | Quit

# how long the demo waits for an operation on a pool, e.g. until a scrub was started
ACTIVITY_TIMEOUT = 300.0

# an operation running on a pool, in the activity panel: its title, the pool and the progress, e.g. "Scrub  tank  31.8%"
WAIT_FOR_ACTIVITY = Wait(
    r"(Scrub|Resilver|TRIM|Device removal|RAIDZ expansion) +\S+ .*\d+(\.\d+)?%", timeout=ACTIVITY_TIMEOUT
)

# the demo
SCRIPT: list[Step] = [
    # the files and their snapshots are loaded
    Wait(r"FILES +1/4"),
    Wait(r"\d+ snapshots"),
    # the activity panel is shown (a scrub can be started after the demo, the time until then is not in the GIF)
    WAIT_FOR_ACTIVITY,
    Ready(),
    # through the files and the snapshots, then back to the files
    Key(DOWN, 3.0),
    Key(DOWN, 0.5),
    Key(DOWN, 0.5),
    Key(DOWN, 0.5),
    Key(TAB, 1.5),
    Key(DOWN, 0.6),
    Key(DOWN, 0.6),
    Key(DOWN, 0.6),
    Key(TAB, 1.5),
    # search for the file until it is the only entry left, so the history is the one of README.md
    Key("/", 1.0, shows=r"Filter: "),
    Search("README.md", until=r"\b1 of \d+ entries", into=r"Filter: ", delay=0.4),
    # ends typing, the filter stays (if it did not, "h" is typed into it, and the history is not shown)
    Key(ENTER, 1.0, into=r"Filter: "),
    Key("h", 1.0, shows=r"History of 'README\.md'"),
    # down the snapshots of the history (4 at the moment, an identical one and the versions before), slow enough to
    # see what changed in each; more presses than snapshots only select the last one longer
    *[Key(DOWN, 2.0 if i == 0 else 1.5, into=r"History of 'README\.md'") for i in range(5)],
    Key(ESC, 2.0, hides=r"History of"),
    # datasets
    Key("2", 1.5, shows=r"DATASETS 2/4"),
    Key(DOWN, 2.0),
    Key(DOWN, 0.6),
    Key(DOWN, 0.6),
    # pools and the actions of the selected pool: the dialog is only opened and closed, no action is chosen
    Key("3", 2.0, shows=r"POOLS +3/4"),
    Key(ENTER, 4.0, shows=r"Select Action"),
    Key(ESC, 3.0, hides=r"Select Action"),
    # the command menu, searching for the scrub actions
    Key(":", 2.5, shows=r" Commands "),
    Key("s", 1.0, into=r" Commands "),
    Key("c", 0.3, into=r" Commands "),
    Key("r", 0.3, into=r" Commands "),
    Key(ESC, 2.5, hides=r" Commands "),
    # the disks of the pools, their temperature and health
    Key("4", 3.0, shows=r"DISKS +4/4"),
    # back to the files
    Key("1", 1.5, shows=r"FILES +1/4"),
    Quit(3.0),
]


class DemoFailed(Exception):
    """The demo cannot go on: a screen did not show up, or the script is not safe to run."""


def check_script(script: list[Step]) -> None:
    """Raises DemoFailed if the script does not start with a Wait (keys typed before the application is ready may be
    read differently, or not at all), if a key other than the navigation keys does not say what it shows, hides or is
    typed into, or if a pattern is not a valid regular expression."""
    if not script or not isinstance(script[0], Wait):
        raise DemoFailed("the script must start with a Wait for the application to be ready")
    for number, step in enumerate(script, start=1):
        if isinstance(step, Key):
            conditions = [pattern for pattern in (step.shows, step.hides, step.into) if pattern is not None]
            if not conditions and step.keys not in NAVIGATION_KEYS:
                raise DemoFailed(f"step {number}: {step.keys!r} must say what it shows, hides or is typed into")
        elif isinstance(step, Wait):
            conditions = [step.pattern]
        elif isinstance(step, Search):
            conditions = [step.until, step.into]
        else:
            conditions = []
        for pattern in conditions:
            try:
                re.compile(pattern)
            except re.error as error:
                raise DemoFailed(f"step {number}: invalid pattern /{pattern}/: {error}") from error


class Driver:
    """Passes the output of the application through and keeps a copy of its screen."""

    def __init__(self, fd: int, columns: int, lines: int, out: BinaryIO) -> None:
        self.fd = fd
        self.out = out
        self.screen = pyte.Screen(columns, lines)
        self.stream = pyte.ByteStream(self.screen)
        # whether the application still runs (its output is not closed)
        self.running = True
        # when the application last wrote something (time.monotonic)
        self.last_output = time.monotonic()

    def screen_text(self) -> str:
        """Returns the text on the screen, without the spaces at the end of the lines (for messages)."""
        return "\n".join(line.rstrip() for line in self.screen.display)

    def shows(self, pattern: str) -> bool:
        # with the spaces at the end of the lines, e.g. after the label of an empty search field
        return re.search(pattern, "\n".join(self.screen.display)) is not None

    def pump(self, until: float) -> None:
        """Passes the output through until the given time (time.monotonic) or until the application quits."""
        while self.running:
            timeout = until - time.monotonic()
            if timeout <= 0:
                return
            ready, _, _ = select.select([self.fd], [], [], timeout)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError:
                # EIO: the application closed the terminal
                data = b""
            if not data:
                self.running = False
                return
            self.out.write(data)
            self.out.flush()
            self.stream.feed(data)
            self.last_output = time.monotonic()

    def sleep(self, seconds: float) -> None:
        self.pump(time.monotonic() + seconds)

    def wait(self, pattern: str, shown: bool = True, timeout: float | None = None) -> None:
        """Waits until the pattern is on the screen (or no longer, if not shown), raises DemoFailed after timeout
        seconds (WAIT_TIMEOUT if None)."""
        timeout = WAIT_TIMEOUT if timeout is None else timeout
        deadline = time.monotonic() + timeout
        while self.shows(pattern) != shown:
            if not self.running or time.monotonic() >= deadline:
                raise self.failure(f"/{pattern}/ did not {'show up' if shown else 'go away'} within {timeout:g} s")
            self.pump(min(time.monotonic() + 0.1, deadline))

    def require(self, pattern: str, shown: bool, key: str) -> None:
        """Raises DemoFailed unless the pattern is on the screen (or not, if not shown) right now."""
        if self.shows(pattern) != shown:
            raise self.failure(f"/{pattern}/ must {'' if shown else 'not '}be shown before typing {key!r}")

    def failure(self, reason: str) -> DemoFailed:
        return DemoFailed(f"{reason}, the screen was:\n{self.screen_text()}")

    def send(self, keys: str) -> None:
        os.write(self.fd, keys.encode())

    def run(self, step: Step) -> None:
        if isinstance(step, Wait):
            self.wait(step.pattern, timeout=step.timeout)
        elif isinstance(step, Key):
            self.sleep(step.delay)
            # checked right before the key: the screen may have changed while sleeping
            if step.shows is not None:
                self.require(step.shows, False, step.keys)
            if step.hides is not None:
                self.require(step.hides, True, step.keys)
            if step.into is not None:
                self.require(step.into, True, step.keys)
            self.send(step.keys)
            if step.shows is not None:
                self.wait(step.shows)
            if step.hides is not None:
                self.wait(step.hides, shown=False)
        elif isinstance(step, Ready):
            deadline = time.monotonic() + WAIT_TIMEOUT
            while (quiet_until := self.last_output + step.quiet) > time.monotonic():
                if not self.running or time.monotonic() >= deadline:
                    raise self.failure(f"the application did not stop drawing within {WAIT_TIMEOUT:g} s")
                self.pump(min(quiet_until, deadline))
            self.out.write(READY_MARKER.encode())
            self.out.flush()
        elif isinstance(step, Search):
            self.require(step.until, False, step.text)
            for length in range(1, len(step.text) + 1):
                self.sleep(step.delay)
                typed = step.text[:length]
                self.require(step.into, True, typed[-1])
                self.send(typed[-1])
                self.wait(step.into + re.escape(typed))
                if self.shows(step.until):
                    return
            raise self.failure(f"/{step.until}/ was not shown after typing {step.text!r}")
        else:
            self.sleep(step.delay)
            self.send(CTRL_Q)
            self.pump(time.monotonic() + QUIT_TIMEOUT)
            if self.running:
                raise self.failure(f"the application did not quit within {QUIT_TIMEOUT:g} s")


def terminal_size() -> tuple[int, int]:
    """Returns the size (columns, lines) of the terminal the driver runs in."""
    try:
        size = os.get_terminal_size(sys.stdout.fileno())
    except OSError:
        return DEFAULT_SIZE
    # a terminal that does not know its size reports 0
    return (size.columns, size.lines) if size.columns > 0 and size.lines > 0 else DEFAULT_SIZE


def stop(pid: int) -> None:
    """Stops the application: SIGTERM, then SIGKILL if it did not quit within QUIT_TIMEOUT."""
    os.kill(pid, signal.SIGTERM)
    deadline = time.monotonic() + QUIT_TIMEOUT
    while time.monotonic() < deadline:
        if os.waitpid(pid, os.WNOHANG) != (0, 0):
            return
        time.sleep(0.1)
    os.kill(pid, signal.SIGKILL)
    os.waitpid(pid, 0)


def run_demo(binary: str, start: str, error_file: str, script: list[Step], out: BinaryIO) -> bool:
    """Runs the application in a pty in the start directory and the script on it, its output is written to out.
    Returns whether it went through; if not, the reason is in the error file."""
    try:
        check_script(script)
    except DemoFailed as error:
        with open(error_file, "w") as file:
            file.write(f"demo not started: {error}\n")
        return False

    with tempfile.TemporaryDirectory(prefix="zfs-file-history-demo-") as state_home:
        columns, lines = terminal_size()
        pid, fd = pty.fork()
        if pid == 0:
            # the child: never return into the code of the driver, also if the application cannot be started
            try:
                os.chdir(start)
                os.execve(binary, [binary, "--fresh"], {**os.environ, "XDG_STATE_HOME": state_home})
            except OSError as error:
                print(f"cannot start {binary}: {error}", file=sys.stderr)
            os._exit(127)
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", lines, columns, 0, 0))

        driver = Driver(fd, columns, lines, out)
        try:
            for step in script:
                driver.run(step)
        except DemoFailed as error:
            with open(error_file, "w") as file:
                file.write(f"demo stopped: {error}\n")
            stop(pid)
            return False
        os.waitpid(pid, 0)
        return True


def main() -> None:
    parser = argparse.ArgumentParser(description="Runs zfs-file-history and types the keys of the demo.")
    parser.add_argument("binary", help="the zfs-file-history binary")
    parser.add_argument("start", help="the directory the application is started in")
    parser.add_argument("error_file", help="where the reason is written if the demo stops")
    parser.add_argument(
        "--without-activity",
        action="store_true",
        help=f"do not wait (up to {ACTIVITY_TIMEOUT:g} s) for an operation on a pool, e.g. a scrub, before the demo",
    )
    args = parser.parse_args()
    script = [step for step in SCRIPT if not (args.without_activity and step is WAIT_FOR_ACTIVITY)]
    # absolute, as the application is started in the start directory
    ok = run_demo(os.path.abspath(args.binary), args.start, os.path.abspath(args.error_file), script, sys.stdout.buffer)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
