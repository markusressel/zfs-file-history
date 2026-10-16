#!/usr/bin/env python3
"""Runs zfs-file-history in a pty and types the keys of a storyboard, for "just demo-record" (asciinema rec --headless).

The output of the application is passed through, so asciinema records it. The storyboards are in storyboards.py: the
overview walks through the whole application (the GIF of the README), the others show one feature each. A storyboard
may wait until a scrub (or another pool operation) runs, so the activity panel is shown (--without-activity does not,
unless the storyboard is about it); the time until then is not in the GIF, see Ready.

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

The driver marks each key and click in the recording (prepare_cast.key_marker), so the GIF shows them (render.py).

usage: uv run driver.py [--storyboard <name>] [--without-activity] <binary> <start directory> <error file>
       uv run driver.py --list
       uv run driver.py --describe <name>   (prints the size of its terminal, e.g. 140x36, whether it waits for an
       activity: required, optional (left out with --without-activity) or no, and for an alert: alert or no)
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
from typing import BinaryIO

import pyte

from prepare_cast import READY_MARKER, ZOOM_OUT_MARKER, key_marker, zoom_marker
from steps import (
    CLICK_LABEL,
    CTRL_Q,
    NAVIGATION_KEYS,
    Click,
    Key,
    Ready,
    Search,
    Select,
    Step,
    Wait,
    Zoom,
    ZoomOut,
    key_label,
)
from storyboards import ACTIVITY_TIMEOUT, STORYBOARDS, WAIT_FOR_ACTIVITY, Storyboard

# the corners of the borders of panes and dialogs, see Driver.box
TOP_LEFT_CORNERS = {"┌", "╔", "╭"}
TOP_RIGHT_CORNERS = {"┐", "╗", "╮"}
BOTTOM_LEFT_CORNERS = {"└", "╚", "╰"}

# the background of the selected row of tables and dialogs (theme.Colors.Layout.Table.SelectedBackground, see
# TestSelectedBackgroundOfTheDemo), as pyte keeps the color of a cell, see Select
SELECTED_BACKGROUND = "2d4560"
# how long the application may take to draw after a key that moves the selection, see Select
SELECT_SETTLE = 0.1

# how long a screen may take to show up (or go away) before the demo is stopped
WAIT_TIMEOUT = 15.0
# how long the application may take to quit, after ctrl+q or SIGTERM
QUIT_TIMEOUT = 5.0
# the size of the terminal if the driver does not run in one (asciinema passes its --window-size)
DEFAULT_SIZE = (179, 42)


class DemoFailed(Exception):
    """The demo cannot go on: a screen did not show up, or the script is not safe to run."""


def check_script(script: list[Step]) -> None:
    """Raises DemoFailed if the script does not start with a Wait (keys typed before the application is ready may be
    read differently, or not at all), if a key other than the navigation keys does not say what it shows, hides or is
    typed into, if a click does not say what it shows or hides, or if a pattern is not a valid regular expression."""
    if not script or not isinstance(script[0], Wait):
        raise DemoFailed("the script must start with a Wait for the application to be ready")
    for number, step in enumerate(script, start=1):
        if isinstance(step, Key):
            conditions = [pattern for pattern in (step.shows, step.hides, step.into) if pattern is not None]
            if not conditions and step.keys not in NAVIGATION_KEYS:
                raise DemoFailed(f"step {number}: {step.keys!r} must say what it shows, hides or is typed into")
        elif isinstance(step, Click):
            conditions = [pattern for pattern in (step.shows, step.hides) if pattern is not None]
            if not conditions:
                raise DemoFailed(f"step {number}: the click on /{step.pattern}/ must say what it shows or hides")
            conditions.append(step.pattern)
        elif isinstance(step, Select):
            if step.key not in NAVIGATION_KEYS:
                raise DemoFailed(f"step {number}: a Select may only move the selection, not type {step.key!r}")
            conditions = [step.pattern, step.into]
        elif isinstance(step, Wait):
            conditions = [step.pattern]
        elif isinstance(step, Zoom):
            conditions = [step.title]
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

    def mark(self, marker: str) -> None:
        """Writes a marker into the recording, see prepare_cast."""
        self.out.write(marker.encode())
        self.out.flush()

    def send(self, keys: str) -> None:
        os.write(self.fd, keys.encode())

    def find(self, pattern: str, line: int | None) -> tuple[int, int] | None:
        """Returns the cell (column, line) of the first match of the pattern on the screen (of its group "at", if it
        has one and it matched), None if there is none."""
        lines = range(self.screen.lines) if line is None else [line]
        for y in lines:
            text = self.screen.display[y]
            match = re.search(pattern, text)
            if match is None:
                continue
            index = match.start("at") if "at" in match.re.groupindex and match.start("at") >= 0 else match.start()
            return self.column(y, index), y
        return None

    def box(self, title: str) -> tuple[int, int, int, int] | None:
        """Returns the cells (left, top, right, bottom) of the corners of the box whose top border shows the pattern
        title (e.g. the title of a dialog), None if there is none."""
        for y in range(self.screen.lines):
            match = re.search(title, self.screen.display[y])
            if match is None:
                continue
            x = self.column(y, match.start())
            row = self.screen.buffer[y]
            left = next((column for column in range(x, -1, -1) if row[column].data in TOP_LEFT_CORNERS), None)
            right = next(
                (column for column in range(x, self.screen.columns) if row[column].data in TOP_RIGHT_CORNERS), None
            )
            if left is None or right is None:
                continue
            bottom = next(
                (
                    line
                    for line in range(y + 1, self.screen.lines)
                    if self.screen.buffer[line][left].data in BOTTOM_LEFT_CORNERS
                ),
                None,
            )
            if bottom is not None:
                return left, y, right, bottom
        return None

    def is_selected(self, pattern: str) -> bool:
        """Returns whether the line of the first match of the pattern is selected: its first cell is drawn on the
        background of the selection."""
        cell = self.find(pattern, None)
        if cell is None:
            return False
        x, y = cell
        return self.screen.buffer[y][x].bg == SELECTED_BACKGROUND

    def select(self, step: Select) -> None:
        """Presses the key of the step until the pattern is selected, see Select."""
        for _ in range(step.max_keys):
            if self.is_selected(step.pattern):
                return
            self.sleep(step.delay)
            self.require(step.into, True, step.key)
            self.mark(key_marker(key_label(step.key)))
            sent = time.monotonic()
            self.send(step.key)
            # the application draws the moved selection; it draws nothing if the selection cannot move further
            deadline = sent + WAIT_TIMEOUT
            while self.last_output < sent:
                if not self.running or time.monotonic() >= deadline:
                    raise self.failure(f"the selection did not move towards /{step.pattern}/")
                self.pump(min(time.monotonic() + 0.1, deadline))
            self.sleep(SELECT_SETTLE)
        if not self.is_selected(step.pattern):
            raise self.failure(f"/{step.pattern}/ was not selected after {step.max_keys} keys")

    def column(self, y: int, index: int) -> int:
        """Returns the column of the character at the index of the text of the line: a wide character (e.g. 名) takes
        two columns, the second one is empty in the text."""
        characters = 0
        for x in range(self.screen.columns):
            if characters >= index:
                return x
            characters += len(self.screen.buffer[y][x].data)
        return self.screen.columns - 1

    def click(self, x: int, y: int, hover: float) -> None:
        """Moves the mouse to the cell (so it is highlighted while hovered), then presses and releases the left button
        there, as SGR mouse events (1-based)."""
        self.send(f"\x1b[<35;{x + 1};{y + 1}M")
        self.sleep(hover)
        self.mark(key_marker(CLICK_LABEL))
        self.send(f"\x1b[<0;{x + 1};{y + 1}M")
        self.send(f"\x1b[<0;{x + 1};{y + 1}m")

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
            self.mark(key_marker(step.keys if step.typed else key_label(step.keys), typed=step.typed))
            self.send(step.keys)
            if step.shows is not None:
                self.wait(step.shows)
            if step.hides is not None:
                self.wait(step.hides, shown=False)
        elif isinstance(step, Click):
            self.sleep(step.delay)
            if step.shows is not None:
                self.require(step.shows, False, step.pattern)
            if step.hides is not None:
                self.require(step.hides, True, step.pattern)
            cell = self.find(step.pattern, step.line)
            if cell is None:
                raise self.failure(f"/{step.pattern}/ to click is not shown")
            self.click(*cell, step.hover)
            if step.shows is not None:
                self.wait(step.shows)
            if step.hides is not None:
                self.wait(step.hides, shown=False)
        elif isinstance(step, Select):
            self.select(step)
        elif isinstance(step, Zoom):
            self.sleep(step.delay)
            box = self.box(step.title)
            if box is None:
                raise self.failure(f"no box with /{step.title}/ in its top border to zoom in to")
            self.mark(zoom_marker(*box, step.padding))
        elif isinstance(step, ZoomOut):
            self.sleep(step.delay)
            self.mark(ZOOM_OUT_MARKER)
        elif isinstance(step, Ready):
            deadline = time.monotonic() + WAIT_TIMEOUT
            while (quiet_until := self.last_output + step.quiet) > time.monotonic():
                if not self.running or time.monotonic() >= deadline:
                    raise self.failure(f"the application did not stop drawing within {WAIT_TIMEOUT:g} s")
                self.pump(min(quiet_until, deadline))
            self.mark(READY_MARKER)
        elif isinstance(step, Search):
            self.require(step.until, False, step.text)
            for length in range(1, len(step.text) + 1):
                self.sleep(step.delay)
                typed = step.text[:length]
                self.require(step.into, True, typed[-1])
                self.mark(key_marker(typed[-1], typed=True))
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


def storyboard_steps(storyboard: Storyboard, without_activity: bool) -> list[Step]:
    """Returns the steps of the storyboard, without the wait for an activity if without_activity (unless the
    storyboard is about it)."""
    if not without_activity or storyboard.needs_activity:
        return storyboard.steps
    return [step for step in storyboard.steps if step is not WAIT_FOR_ACTIVITY]


def main() -> None:
    parser = argparse.ArgumentParser(description="Runs zfs-file-history and types the keys of a storyboard.")
    parser.add_argument("--list", action="store_true", help="list the storyboards and exit")
    parser.add_argument(
        "--describe",
        metavar="NAME",
        help="print the terminal size of the storyboard (e.g. 140x36), whether it waits for an activity "
        "(required, optional or no) and for an alert (alert or no), and exit",
    )
    parser.add_argument("--storyboard", default="overview", choices=sorted(STORYBOARDS), help="what to record")
    parser.add_argument(
        "--without-activity",
        action="store_true",
        help=f"do not wait (up to {ACTIVITY_TIMEOUT:g} s) for an operation on a pool, e.g. a scrub, before the demo",
    )
    parser.add_argument("binary", nargs="?", help="the zfs-file-history binary")
    parser.add_argument("start", nargs="?", help="the directory the application is started in")
    parser.add_argument("error_file", nargs="?", help="where the reason is written if the demo stops")
    args = parser.parse_args()
    if args.list:
        for storyboard in STORYBOARDS.values():
            print(f"{storyboard.name:14} {storyboard.description}")
        return
    if args.describe is not None:
        if args.describe not in STORYBOARDS:
            parser.error(f"unknown storyboard {args.describe!r}, see --list")
        storyboard = STORYBOARDS[args.describe]
        columns, lines = storyboard.size
        if storyboard.needs_activity:
            activity = "required"
        elif WAIT_FOR_ACTIVITY in storyboard.steps:
            activity = "optional"
        else:
            activity = "no"
        print(f"{columns}x{lines} {activity} {'alert' if storyboard.needs_alert else 'no'}")
        return
    if args.error_file is None:
        parser.error("binary, start and error_file are required")
    script = storyboard_steps(STORYBOARDS[args.storyboard], args.without_activity)
    # absolute, as the application is started in the start directory
    ok = run_demo(os.path.abspath(args.binary), args.start, os.path.abspath(args.error_file), script, sys.stdout.buffer)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
