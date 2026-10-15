#!/usr/bin/env python3
"""Renders the GIF of the demo from the large GIF of agg with ffmpeg: zooms in to boxes (e.g. the command menu) and
out again (see steps.Zoom), and shows the keys typed in the bottom right corner (see captions).

agg renders the recording at twice the font size (see the Justfile), the GIF is scaled to half of it, so text that is
zoomed in up to twice stays sharp. Each zoom eases in over TRANSITION seconds to its box (the cells of
<prepared .cast>.render.json, written by prepare_cast.py), as large as it fits the GIF (at most MAX_ZOOM), and eases out
again at its end. The size of the cells in pixels is the one of agg (its default font, see CELL_WIDTH). The keys are
drawn after zooming, so they stay in the corner and keep their size.

usage: render.py <render.json> <font size of the large GIF> <large GIF> <GIF>
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import tempfile
from dataclasses import dataclass

# the size of a cell of agg 1.9.0 (its default font), relative to the font size, and its margin around the terminal
CELL_WIDTH = 0.602679
CELL_HEIGHT = 1.404762
# frames per second: the most GIFs play everywhere (delays are hundredths of a second, browsers slow down shorter ones
# than 2), so zooming is smooth; frames that do not change are dropped again (mpdecimate), so the rest keeps the
# frames of agg (--fps-cap)
FPS = 50
# how long zooming in or out takes, in seconds
TRANSITION = 0.5
# how far it zooms in at most: the large GIF has twice the resolution of the GIF
MAX_ZOOM = 2.0
# how far it zooms in at least, closer boxes are not zoomed in to
MIN_ZOOM = 1.05

# how long the keys stay after the last one, in seconds: a key within this time is added to the keys shown
KEYS_HOLD = 1.5
# the most keys shown at once (repeated keys count once), the oldest are dropped
KEYS_SHOWN = 4
# how long the keys take to show up and to go away, in seconds
KEYS_FADE_IN = 0.1
KEYS_FADE_OUT = 0.25
# the font of the keys (fontconfig), with the arrows; its size, the padding of its box and the margin of the box to
# the corner, in pixels of the GIF
KEYS_FONT = "DejaVu Sans Mono:bold"
KEYS_FONT_SIZE = 22
KEYS_PADDING = 10
KEYS_MARGIN = 14
# the colors of the keys and their box, in front of the dark terminal
KEYS_COLOR = "0xffffff"
KEYS_BOX_COLOR = "0x2b2b2b@0.92"
# the sign between a repeated key and its count (multiplication sign)
TIMES = "\u00d7"


@dataclass(frozen=True)
class Geometry:
    """Where the cells of the terminal are in the large GIF."""

    width: int
    height: int
    columns: int
    lines: int
    font_size: float

    @property
    def cell_width(self) -> float:
        return CELL_WIDTH * self.font_size

    @property
    def cell_height(self) -> float:
        return CELL_HEIGHT * self.font_size

    def pixels(self, left: int, top: int, right: int, bottom: int) -> tuple[float, float, float, float]:
        """Returns the pixels (left, top, right, bottom) of the cells, from their first to after their last."""
        margin_x = (self.width - self.columns * self.cell_width) / 2
        margin_y = (self.height - self.lines * self.cell_height) / 2
        return (
            margin_x + left * self.cell_width,
            margin_y + top * self.cell_height,
            margin_x + (right + 1) * self.cell_width,
            margin_y + (bottom + 1) * self.cell_height,
        )


def ease(linear: str) -> str:
    """Returns an ffmpeg expression that eases the linear progress (0 to 1) in and out (cubic): it starts and ends
    slowly, like a camera that starts and stops moving."""
    return f"if(lt({linear},0.5),4*pow({linear},3),1-pow(2-2*{linear},3)/2)"


def progress(start: float, end: float) -> str:
    """Returns an ffmpeg expression of how far a zoom from start to end (seconds) is in at the current frame: 0
    before, easing to 1 over TRANSITION, back to 0 from end on."""
    time = f"(in/{FPS})"
    linear = f"min(clip(({time}-{start:.3f})/{TRANSITION},0,1),clip(({end:.3f}+{TRANSITION}-{time})/{TRANSITION},0,1))"
    return f"({ease(linear)})"


def zoompan(zooms: list[dict], geometry: Geometry) -> str:
    """Returns the zoompan filter that zooms in to the zooms (see prepare_cast.extract_markers), with the size of the
    large GIF.

    The zoom grows by the same factor in each frame of the same progress (the factor to the power of the progress),
    so it looks as fast when it starts as when it ends; growing linearly, it would rush in and crawl at the end. It
    zooms around the point that stays in place between the whole frame and the box, so it does not pan sideways
    while zooming."""
    zoom_terms, x_terms, y_terms = [], [], []
    for zoom in zooms:
        left, top, right, bottom = geometry.pixels(zoom["left"], zoom["top"], zoom["right"], zoom["bottom"])
        factor = min(geometry.width / (right - left), geometry.height / (bottom - top), MAX_ZOOM)
        if factor < MIN_ZOOM:
            # e.g. a box that fills the screen: the frame would only shake
            continue
        # the window (a 1/factor of the frame) around the center of the box, within the frame, and the point that
        # stays in place while zooming in to it: the window's corner is that much (1 - 1/zoom) of the way to it
        window_width, window_height = geometry.width / factor, geometry.height / factor
        window_x = min(max((left + right) / 2 - window_width / 2, 0), geometry.width - window_width)
        window_y = min(max((top + bottom) / 2 - window_height / 2, 0), geometry.height - window_height)
        fixed_x, fixed_y = window_x / (1 - 1 / factor), window_y / (1 - 1 / factor)
        amount = progress(zoom["start"], zoom["end"])
        zoom_terms.append(f"pow({factor:.4f},{amount})")
        # zooms do not overlap, only the current one moves the window
        x_terms.append(f"if(gt({amount},0),{fixed_x:.2f})")
        y_terms.append(f"if(gt({amount},0),{fixed_y:.2f})")
    if not zoom_terms:
        zoom_expression, x, y = "1", "0", "0"
    else:
        zoom_expression = "*".join(zoom_terms)
        x = f"max(0,min(iw-iw/zoom,({'+'.join(x_terms)})*(1-1/zoom)))"
        y = f"max(0,min(ih-ih/zoom,({'+'.join(y_terms)})*(1-1/zoom)))"
    size = f"{geometry.width // 2}x{geometry.height // 2}"
    return f"zoompan=z='{zoom_expression}':x='{x}':y='{y}':d=1:s={size}:fps={FPS}"


@dataclass(frozen=True)
class Caption:
    """The keys shown from start to end (seconds), fading in at the start and out at the end if they do."""

    start: float
    end: float
    text: str
    fade_in: bool
    fade_out: bool


def captions(keys: list[dict], end: float) -> list[Caption]:
    """Returns what the overlay shows for the keys (see prepare_cast.extract_markers): each key shows the keys since
    the overlay came up (at most KEYS_SHOWN, repeated ones with their count, typed text as one, "internal") until the
    next key, or KEYS_HOLD seconds, then it goes away. end is the end of the GIF."""
    result: list[Caption] = []
    # the keys shown: [label, count, typed]
    shown: list[list] = []
    for index, key in enumerate(keys):
        fade_in = index == 0 or key["time"] - keys[index - 1]["time"] > KEYS_HOLD
        if fade_in:
            shown = []
        last = shown[-1] if shown else None
        if last is not None and key["typed"] and last[2]:
            last[0] += key["label"]
        elif last is not None and not key["typed"] and not last[2] and last[0] == key["label"]:
            last[1] += 1
        else:
            shown.append([key["label"], 1, key["typed"]])
        del shown[:-KEYS_SHOWN]
        following = keys[index + 1]["time"] if index + 1 < len(keys) else None
        fade_out = following is None or following - key["time"] > KEYS_HOLD
        stop = min(key["time"] + KEYS_HOLD, end) if fade_out else following
        text = "  ".join(label + (f" {TIMES}{count}" if count > 1 else "") for label, count, _ in shown)
        if stop > key["time"]:
            result.append(Caption(key["time"], stop, text, fade_in, fade_out))
    return result


def drawtext(caption: Caption, text_file: str, font_file: str) -> str:
    """Returns the drawtext filter that shows the caption (its text in text_file) in the bottom right corner."""
    alpha = "1"
    if caption.fade_in:
        alpha = f"min({alpha},(t-{caption.start:.3f})/{KEYS_FADE_IN})"
    if caption.fade_out:
        alpha = f"min({alpha},({caption.end:.3f}-t)/{KEYS_FADE_OUT})"
    corner = KEYS_MARGIN + KEYS_PADDING
    return (
        f"drawtext=fontfile='{font_file}':textfile='{text_file}':expansion=none:fontsize={KEYS_FONT_SIZE}"
        f":fontcolor={KEYS_COLOR}:box=1:boxcolor={KEYS_BOX_COLOR}:boxborderw={KEYS_PADDING}:y_align=font"
        f":x=w-tw-{corner}:y=h-th-{corner}:alpha='max(0,{alpha})'"
        f":enable='between(t,{caption.start:.3f},{caption.end:.3f})'"
    )


def gif_info(path: str) -> tuple[int, int, float]:
    """Returns the width, height and duration (seconds) of the GIF."""
    output = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-select_streams",
            "v",
            "-show_entries",
            "stream=width,height:format=duration",
            "-of",
            "default=noprint_wrappers=1",
            path,
        ],
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    values = dict(line.split("=", 1) for line in output.split())
    return int(values["width"]), int(values["height"]), float(values["duration"])


def font_file(pattern: str) -> str:
    return subprocess.run(
        ["fc-match", "--format=%{file}", pattern], check=True, capture_output=True, text=True
    ).stdout.strip()


def render(render_file: str, font_size: float, source: str, target: str) -> None:
    """Writes the large GIF source, zoomed, with the keys and scaled to half, to target."""
    with open(render_file) as file:
        description = json.load(file)
    width, height, duration = gif_info(source)
    geometry = Geometry(width, height, description["columns"], description["lines"], font_size)
    with tempfile.TemporaryDirectory() as directory:
        # the texts in files: drawtext's own escaping of text in a filter graph is fragile
        keys = []
        font = font_file(KEYS_FONT)
        for index, caption in enumerate(captions(description["keys"], duration)):
            text_file = os.path.join(directory, f"{index}.txt")
            with open(text_file, "w") as file:
                file.write(caption.text)
            keys.append(drawtext(caption, text_file, font))
        # a frame for every 1/FPS s (zoompan takes one per frame), the keys, frames that did not change dropped again
        # (the GIF keeps their time; only identical ones), one palette for the whole GIF, without dithering (sharp text)
        filters = [f"fps={FPS}", zoompan(description["zooms"], geometry), *keys, "mpdecimate=hi=0:lo=0:frac=0"]
        graph = (
            f"[0:v]{','.join(filters)},split[frames][palette];"
            "[palette]palettegen=stats_mode=full[colors];[frames][colors]paletteuse=dither=none"
        )
        subprocess.run(
            ["ffmpeg", "-loglevel", "error", "-y", "-i", source, "-filter_complex", graph, "-fps_mode", "vfr", target],
            check=True,
        )


def main() -> None:
    parser = argparse.ArgumentParser(description="Renders the GIF of the demo: zooms and the keys typed.")
    parser.add_argument("render_file", help="the zooms and keys, written by prepare_cast.py")
    parser.add_argument("font_size", type=float, help="the font size agg rendered the large GIF with")
    parser.add_argument("source", help="the large GIF")
    parser.add_argument("target", help="the GIF")
    args = parser.parse_args()
    render(args.render_file, args.font_size, args.source, args.target)


if __name__ == "__main__":
    main()
