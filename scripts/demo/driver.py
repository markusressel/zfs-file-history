#!/usr/bin/env python3
"""Runs zfs-file-history in a pty and types the keys of the demo, for "just demo-record" (asciinema rec --headless).

The output of the application is passed through, so asciinema records it. The demo shows the files page and the
history of a file, the datasets page, the pools page with its actions, and the command menu. Run it while a scrub
(or another pool operation) is running, so the activity panel is shown.

usage: driver.py <binary> <start directory>
"""
import fcntl
import os
import pty
import select
import struct
import sys
import termios
import time

UP, DOWN, RIGHT, LEFT = "\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D"
TAB, ENTER, ESC = "\t", "\r", "\x1b"
CTRL_Q = "\x11"

# the keys of the demo: (seconds to wait before, keys)
SCRIPT = [
    (3.0, DOWN), (0.5, DOWN), (0.5, DOWN),
    (1.0, RIGHT), (1.0, DOWN), (0.5, DOWN),
    (1.5, TAB), (0.6, DOWN), (0.6, DOWN), (0.6, DOWN),
    (1.5, TAB),
    (1.0, "h"),
    (3.0, ESC),
    (1.5, "2"),
    (2.0, DOWN), (0.6, DOWN), (0.6, DOWN),
    (2.0, "3"),
    (4.0, ENTER),
    (3.0, ESC),
    (2.5, ":"),
    (1.0, "s"), (0.3, "c"), (0.3, "r"),
    (2.5, ESC),
    (1.5, "1"),
    (3.0, CTRL_Q),
]


def window_size():
    try:
        return fcntl.ioctl(sys.stdout.fileno(), termios.TIOCGWINSZ, b"\0" * 8)
    except OSError:
        return struct.pack("HHHH", 42, 179, 0, 0)


def main():
    # absolute, as the application is started in the start directory
    binary, start = os.path.abspath(sys.argv[1]), sys.argv[2]
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(start)
        os.execv(binary, [binary, "--fresh"])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, window_size())

    steps = list(SCRIPT)
    next_at = time.monotonic() + steps[0][0]
    out = sys.stdout.buffer
    while True:
        timeout = max(next_at - time.monotonic(), 0) if steps else 0.1
        ready, _, _ = select.select([fd], [], [], timeout)
        if ready:
            try:
                data = os.read(fd, 65536)
            except OSError:
                break
            if not data:
                break
            out.write(data)
            out.flush()
        if steps and time.monotonic() >= next_at:
            _, keys = steps.pop(0)
            os.write(fd, keys.encode())
            if steps:
                next_at = time.monotonic() + steps[0][0]
    os.waitpid(pid, 0)


if __name__ == "__main__":
    main()
