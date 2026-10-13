#!/usr/bin/env python3
"""Prepares a recording of the demo for agg, which renders it as a GIF.

The shortcut bar separates the key and the name of each entry with a non-breaking space, which agg draws as a
stray glyph with some fonts. It looks the same as a space in a terminal, so it is replaced by one.

The recording ends with the application quitting: it clears the screen and leaves the alternate screen, which shows
the empty terminal for a moment. Everything after the last frame of the application is dropped, so the GIF loops
from the last frame straight back to the first.

usage: prepare_cast.py <input .cast> <output .cast>
"""
import json
import sys

NBSP = " "
# leaving the alternate screen, the application quits
LEAVE_ALTERNATE_SCREEN = "\x1b[?1049l"
# the end of a synchronized update, after each frame
END_OF_FRAME = "\x1b[?2026l"


def main():
    source, target = sys.argv[1], sys.argv[2]
    with open(source) as input_file, open(target, "w") as output_file:
        for number, line in enumerate(input_file):
            if number > 0 and line.strip():
                event = json.loads(line)
                if event[1] == "o":
                    event[2] = event[2].replace(NBSP, " ")
                    quit_at = event[2].find(LEAVE_ALTERNATE_SCREEN)
                    if quit_at >= 0:
                        # the teardown before it (cursor, modes, clearing the screen) is dropped too
                        frame_end = event[2].rfind(END_OF_FRAME, 0, quit_at)
                        event[2] = event[2][:frame_end + len(END_OF_FRAME) if frame_end >= 0 else quit_at]
                        output_file.write(json.dumps(event) + "\n")
                        break
                line = json.dumps(event) + "\n"
            output_file.write(line)


if __name__ == "__main__":
    main()
