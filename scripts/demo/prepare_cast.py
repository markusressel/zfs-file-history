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

The driver also marks where the GIF zooms in to a box and out again (see steps.Zoom), and the keys it types. The
markers are removed, and their times in the GIF (agg shortens pauses to IDLE_TIME_LIMIT) are written with the boxes and
the keys to <output>.render.json, for render.py.

usage: prepare_cast.py <input .cast> <output .cast>
"""

import argparse
import json
import re
from urllib.parse import quote, unquote

NBSP = "\u00a0"
# leaving the alternate screen, the application quits
LEAVE_ALTERNATE_SCREEN = "\x1b[?1049l"
# the end of a synchronized update, after each frame
END_OF_FRAME = "\x1b[?2026l"
# written by the driver when the application is ready (see driver.Ready): an OSC sequence, which terminals ignore,
# and which is removed from the recording
READY_MARKER = "\x1b]zfs-file-history-demo;ready\x07"
# written by the driver where the GIF zooms in to a box and out again (see steps.Zoom), removed like READY_MARKER
ZOOM_MARKER_PATTERN = re.compile(r"\x1b\]zfs-file-history-demo;zoom;(\d+);(\d+);(\d+);(\d+);(\d+)\x07")
ZOOM_OUT_MARKER = "\x1b]zfs-file-history-demo;zoom-out\x07"
# written by the driver when it types a key (or text, one character at a time, or clicks), removed like READY_MARKER;
# the label is percent-encoded, so it cannot end the sequence
KEY_MARKER_PATTERN = re.compile(r"\x1b\]zfs-file-history-demo;(key|text);([^\x07]*)\x07")
# the longest pause in the GIF, in seconds: agg shortens longer ones (its --idle-time-limit, see the Justfile)
IDLE_TIME_LIMIT = 2.0


def zoom_marker(left: int, top: int, right: int, bottom: int, padding: int) -> str:
    """Returns the marker of a zoom in to the box of the cells (its corners), with padding cells around it."""
    return f"\x1b]zfs-file-history-demo;zoom;{left};{top};{right};{bottom};{padding}\x07"


def key_marker(label: str, typed: bool = False) -> str:
    """Returns the marker of a key with the label (e.g. "↓"), or of a character typed as text (shown as one word)."""
    return f"\x1b]zfs-file-history-demo;{'text' if typed else 'key'};{quote(label, safe='')}\x07"


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
    become 0), and removes the marker and the keys typed before it (they are not in the GIF). Returns the events
    unchanged without a marker."""
    ready_index = next(
        (index for index, (_, code, data) in enumerate(events) if code == "o" and READY_MARKER in data), None
    )
    if ready_index is None:
        return events
    before = [[0.0, code, KEY_MARKER_PATTERN.sub("", data)] for _, code, data in events[:ready_index]]
    _, code, data = events[ready_index]
    drawn, after = data.split(READY_MARKER, 1)
    result = [*before, [0.0, code, KEY_MARKER_PATTERN.sub("", drawn)]]
    if after:
        result.append([0.0, code, after])
    return result + events[ready_index + 1 :]


def extract_markers(events: list[Event], columns: int, lines: int) -> tuple[list[Event], list[dict], list[dict]]:
    """Removes the zoom and key markers from the events, and returns them with the zooms and the keys. Zooms: the time
    in the GIF where each starts and ends (pauses shortened to IDLE_TIME_LIMIT, like agg does), and the cells it shows
    (the box and its padding, within the screen); a zoom without an end ends with the recording. Keys: the time in
    the GIF, the label, and whether it was typed as text."""
    zooms: list[dict] = []
    keys: list[dict] = []
    result: list[Event] = []
    time = 0.0
    for interval, code, data in events:
        time += min(interval, IDLE_TIME_LIMIT)
        if code == "o":
            for match in ZOOM_MARKER_PATTERN.finditer(data):
                left, top, right, bottom, padding = (int(value) for value in match.groups())
                zooms.append(
                    {
                        "start": time,
                        "end": None,
                        "left": max(left - padding, 0),
                        "top": max(top - padding, 0),
                        "right": min(right + padding, columns - 1),
                        "bottom": min(bottom + padding, lines - 1),
                    }
                )
            if ZOOM_OUT_MARKER in data and zooms and zooms[-1]["end"] is None:
                zooms[-1]["end"] = time
            for match in KEY_MARKER_PATTERN.finditer(data):
                keys.append({"time": time, "label": unquote(match[2]), "typed": match[1] == "text"})
            data = KEY_MARKER_PATTERN.sub("", ZOOM_MARKER_PATTERN.sub("", data)).replace(ZOOM_OUT_MARKER, "")
        result.append([interval, code, data])
    for zoom in zooms:
        if zoom["end"] is None:
            zoom["end"] = time
    return result, zooms, keys


def prepare(source: str, target: str) -> None:
    """Writes the recording source, prepared for agg, to target, and the zooms and keys to target.render.json."""
    with open(source) as input_file:
        header, *lines = input_file.read().splitlines()
    # the times of version 3 are intervals since the previous event, see start_when_ready
    if json.loads(header).get("version") != 3:
        raise SystemExit(f"{source}: only asciicast version 3 (asciinema 3) is supported")
    events = prepare_events([json.loads(line) for line in lines if line.strip()])
    terminal = json.loads(header)["term"]
    events, zooms, keys = extract_markers(events, terminal["cols"], terminal["rows"])
    with open(target + ".render.json", "w") as render_file:
        description = {"columns": terminal["cols"], "lines": terminal["rows"], "zooms": zooms, "keys": keys}
        json.dump(description, render_file, indent=2, ensure_ascii=False)
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
