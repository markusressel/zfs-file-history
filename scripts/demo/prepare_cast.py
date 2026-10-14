#!/usr/bin/env python3
"""Prepares a recording of the demo for agg, which renders it as a GIF.

The shortcut bar separates the key and the name of each entry with a non-breaking space, which agg draws as a
stray glyph with some fonts. It looks the same as a space in a terminal, so it is replaced by one.

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


Event = list  # [time, code, data] of asciicast, e.g. [0.5, "o", "output"]


def prepare_events(events: list[Event]) -> list[Event]:
    """Returns the events of a recording, prepared for agg (see the module comment)."""
    events = [[time, code, data.replace(NBSP, " ") if code == "o" else data] for time, code, data in events]
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


def prepare(source: str, target: str) -> None:
    """Writes the recording source, prepared for agg, to target."""
    with open(source) as input_file:
        header, *lines = input_file.read().splitlines()
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
