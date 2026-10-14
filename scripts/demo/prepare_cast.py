#!/usr/bin/env python3
"""Prepares a recording of the demo for agg, which renders it as a GIF.

The shortcut bar separates the key and the name of each entry with a non-breaking space, which agg draws as a
stray glyph with some fonts. It looks the same as a space in a terminal, so it is replaced by one.

The recording starts with the terminal before the application drew anything, and its layout while it loads. The
driver marks when the application is ready (READY_MARKER, see driver.Ready): everything before it is moved to the
start of the recording, so the GIF starts with the ready application.

The recording ends with the application quitting: it clears the screen and leaves the alternate screen, which shows
the empty terminal for a moment. Everything after the last frame of the application is dropped, so the GIF loops
from the last frame straight back to the first.

usage: prepare_cast.py <input .cast> <output .cast>
"""

import argparse
import json

NBSP = "\u00a0"
# leaving the alternate screen, the application quits
LEAVE_ALTERNATE_SCREEN = "\x1b[?1049l"
# the end of a synchronized update, after each frame
END_OF_FRAME = "\x1b[?2026l"
# written by the driver when the application is ready (see driver.Ready): an OSC sequence, which terminals ignore,
# and which is removed from the recording
READY_MARKER = "\x1b]zfs-file-history-demo;ready\x07"


Event = list  # [time, code, data] of asciicast, e.g. [0.5, "o", "output"]


def prepare_events(events: list[Event]) -> list[Event]:
    """Returns the events of a recording, prepared for agg (see the module comment)."""
    events = [[time, code, data.replace(NBSP, " ") if code == "o" else data] for time, code, data in events]
    events = start_when_ready(events)
    for quit_index, (_, code, data) in enumerate(events):
        quit_at = data.find(LEAVE_ALTERNATE_SCREEN) if code == "o" else -1
        if quit_at < 0:
            continue
        # The teardown before it (cursor, modes, clearing the screen) is dropped too. It may be in the events before:
        # the output is split into events as the application writes it, not by frame.
        end = quit_at
        for index in range(quit_index, -1, -1):
            time, code, data = events[index]
            frame_end = data.rfind(END_OF_FRAME, 0, end) if code == "o" else -1
            if frame_end >= 0:
                return [*events[:index], [time, code, data[: frame_end + len(END_OF_FRAME)]]]
            end = None
        # no frame was finished: only what follows leaving the alternate screen is dropped
        time, code, data = events[quit_index]
        return [*events[:quit_index], [time, code, data[:quit_at]]]
    return events


def start_when_ready(events: list[Event]) -> list[Event]:
    """Moves the events up to READY_MARKER to the start of the recording (asciicast v3 times are intervals: they
    become 0), and removes the marker. Returns the events unchanged without a marker."""
    ready_index = next(
        (index for index, (_, code, data) in enumerate(events) if code == "o" and READY_MARKER in data), None
    )
    if ready_index is None:
        return events
    before = [[0.0, code, data] for _, code, data in events[:ready_index]]
    _, code, data = events[ready_index]
    drawn, after = data.split(READY_MARKER, 1)
    result = [*before, [0.0, code, drawn]]
    if after:
        result.append([0.0, code, after])
    return result + events[ready_index + 1 :]


def prepare(source: str, target: str) -> None:
    """Writes the recording source, prepared for agg, to target."""
    with open(source) as input_file:
        header, *lines = input_file.read().splitlines()
    # the times of version 3 are intervals since the previous event, see start_when_ready
    if json.loads(header).get("version") != 3:
        raise SystemExit(f"{source}: only asciicast version 3 (asciinema 3) is supported")
    events = prepare_events([json.loads(line) for line in lines if line.strip()])
    with open(target, "w") as output_file:
        output_file.write(header + "\n")
        for event in events:
            output_file.write(json.dumps(event) + "\n")


def main() -> None:
    parser = argparse.ArgumentParser(description="Prepares a recording of the demo for agg.")
    parser.add_argument("source", help="the recording (.cast)")
    parser.add_argument("target", help="the prepared recording (.cast)")
    args = parser.parse_args()
    prepare(args.source, args.target)


if __name__ == "__main__":
    main()
