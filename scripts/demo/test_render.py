"""Tests of rendering the GIF (zooms, keys): "just demo-test"."""

import math
import re
import unittest

from render import (
    FPS,
    KEYS_HOLD,
    KEYS_SHOWN,
    MAX_ZOOM,
    TIMES,
    TRANSITION,
    Caption,
    Geometry,
    captions,
    drawtext,
    progress,
    zoompan,
)

# agg 1.9.0 at font size 28, a terminal of 140x36 (measured)
GEOMETRY = Geometry(2394, 1450, 140, 36, 28)


def evaluate(expression: str, frame: float, zoom: float = 1.0) -> float:
    """Evaluates an expression of zoompan in Python, at the frame (and the zoom, for x and y)."""
    python = (
        expression.replace("in/", f"{frame}/").replace("iw", str(GEOMETRY.width)).replace("ih", str(GEOMETRY.height))
    )
    python = re.sub(r"\b(clip|if|lt|gt|pow)\(", r"_\1(", python)
    functions = {
        "_clip": lambda value, low, high: min(max(value, low), high),
        "_if": lambda condition, then, otherwise=0: then if condition else otherwise,
        "_lt": lambda a, b: 1 if a < b else 0,
        "_gt": lambda a, b: 1 if a > b else 0,
        "_pow": math.pow,
        "min": min,
        "max": max,
        "zoom": zoom,
    }
    return eval(python, functions)


def expressions(filter_: str) -> tuple[str, str, str]:
    """Returns the zoom, x and y expressions of the zoompan filter."""
    return tuple(re.search(rf"{name}='([^']*)'", filter_)[1] for name in ("z", "x", "y"))


class GeometryTest(unittest.TestCase):
    def test_the_cells_in_pixels(self) -> None:
        left, top, right, bottom = GEOMETRY.pixels(0, 0, 139, 35)
        # the margin of agg around the terminal, the same on both sides
        self.assertAlmostEqual(left, 2394 - right, delta=1)
        self.assertAlmostEqual(top, 1450 - bottom, delta=1)
        self.assertTrue(10 < left < 40 and 10 < top < 40)


class ProgressTest(unittest.TestCase):
    def test_eases_in_holds_and_eases_out(self) -> None:
        expression = progress(2.0, 5.0)
        at = lambda seconds: evaluate(expression, seconds * FPS)  # noqa: E731
        self.assertEqual(0, at(1.0))
        self.assertEqual(0, at(2.0))
        self.assertAlmostEqual(0.5, at(2.0 + TRANSITION / 2))
        # slow at the start and the end, fast in the middle
        self.assertLess(at(2.0 + TRANSITION / 10), 0.1 / 2)
        self.assertGreater(at(2.0 + TRANSITION * 0.6) - at(2.0 + TRANSITION * 0.4), 0.2)
        self.assertEqual(1, at(2.0 + TRANSITION))
        self.assertEqual(1, at(5.0))
        self.assertAlmostEqual(0.5, at(5.0 + TRANSITION / 2))
        self.assertEqual(0, at(5.0 + TRANSITION))


class ZoompanTest(unittest.TestCase):
    def test_without_zooms_the_whole_frame_at_half_the_size(self) -> None:
        self.assertIn("z='1'", zoompan([], GEOMETRY))
        self.assertIn("s=1197x725", zoompan([], GEOMETRY))

    def test_zooms_in_to_the_box_at_most_twice(self) -> None:
        small = {"start": 1.0, "end": 2.0, "left": 60, "top": 15, "right": 70, "bottom": 20}
        zoom, _, _ = expressions(zoompan([small], GEOMETRY))
        self.assertAlmostEqual(MAX_ZOOM, evaluate(zoom, round((1.0 + TRANSITION) * FPS)))
        self.assertEqual(1, evaluate(zoom, 0))

    def test_zooms_by_the_same_factor_for_the_same_progress(self) -> None:
        small = {"start": 1.0, "end": 2.0, "left": 60, "top": 15, "right": 70, "bottom": 20}
        zoom, _, _ = expressions(zoompan([small], GEOMETRY))
        # halfway (eased: half of the progress), half of the factor of 2 on a logarithmic scale
        self.assertAlmostEqual(math.sqrt(MAX_ZOOM), evaluate(zoom, (1.0 + TRANSITION / 2) * FPS))

    def test_a_box_wider_than_tall_fits_its_width(self) -> None:
        wide = {"start": 0.0, "end": 9.0, "left": 20, "top": 20, "right": 119, "bottom": 35}
        zoom, _, _ = expressions(zoompan([wide], GEOMETRY))
        # the box (100 columns) as wide as the GIF, with the margin of agg
        self.assertAlmostEqual(2394 / (100 * GEOMETRY.cell_width), evaluate(zoom, round(2 * FPS)), places=3)

    def test_ends_on_the_box_and_zooms_around_a_point_that_stays_in_place(self) -> None:
        box = {"start": 1.0, "end": 5.0, "left": 30, "top": 10, "right": 60, "bottom": 20}
        zoom, x, y = expressions(zoompan([box], GEOMETRY))
        left, top, right, bottom = GEOMETRY.pixels(30, 10, 60, 20)
        frames = [round((1.0 + TRANSITION * part / 4) * FPS) for part in range(1, 5)]
        windows = []
        for frame in frames:
            factor = evaluate(zoom, frame)
            windows.append((factor, evaluate(x, frame, factor), evaluate(y, frame, factor)))
        # zoomed in: the box in the middle of the window
        factor, window_x, window_y = windows[-1]
        self.assertAlmostEqual((left + right) / 2, window_x + GEOMETRY.width / factor / 2, places=1)
        self.assertAlmostEqual((top + bottom) / 2, window_y + GEOMETRY.height / factor / 2, places=1)
        # the point shown at the same place of the GIF in all frames
        fixed_x = window_x / (1 - 1 / factor)
        for factor, window_x, _ in windows:
            self.assertAlmostEqual(fixed_x, (fixed_x - window_x) * factor, places=1)

    def test_the_whole_screen_is_not_zoomed(self) -> None:
        whole = {"start": 0.0, "end": 9.0, "left": 0, "top": 0, "right": 139, "bottom": 35}
        self.assertIn("z='1'", zoompan([whole], GEOMETRY))


def key(time: float, label: str, typed: bool = False) -> dict:
    return {"time": time, "label": label, "typed": typed}


class CaptionsTest(unittest.TestCase):
    def test_adds_keys_until_a_pause_and_counts_repeated_ones(self) -> None:
        keys = [key(1.0, "↓"), key(2.0, "↓"), key(3.0, "Tab"), key(3.0 + KEYS_HOLD + 1, "v")]

        self.assertEqual(
            [
                Caption(1.0, 2.0, "↓", fade_in=True, fade_out=False),
                Caption(2.0, 3.0, f"↓ {TIMES}2", fade_in=False, fade_out=False),
                Caption(3.0, 3.0 + KEYS_HOLD, f"↓ {TIMES}2  Tab", fade_in=False, fade_out=True),
                Caption(4.0 + KEYS_HOLD, 5.0 + KEYS_HOLD, "v", fade_in=True, fade_out=True),
            ],
            captions(keys, end=5.0 + KEYS_HOLD),
        )

    def test_shows_typed_text_as_one_word(self) -> None:
        keys = [key(1.0, "/"), key(1.3, "i", typed=True), key(1.6, "n", typed=True), key(2.0, "Enter")]

        self.assertEqual("/  in  Enter", captions(keys, end=9.0)[-1].text)

    def test_shows_the_last_keys(self) -> None:
        labels = ["a", "b", "c", "d", "e", "f"]
        keys = [key(1.0 + index / 10, label) for index, label in enumerate(labels)]

        self.assertEqual("  ".join(labels[-KEYS_SHOWN:]), captions(keys, end=9.0)[-1].text)

    def test_ends_with_the_gif(self) -> None:
        self.assertEqual([Caption(1.0, 1.2, "q", fade_in=True, fade_out=True)], captions([key(1.0, "q")], end=1.2))
        self.assertEqual([], captions([key(1.0, "q")], end=1.0))


class DrawtextTest(unittest.TestCase):
    def test_shows_the_caption_only_in_its_time_and_fades(self) -> None:
        filter_ = drawtext(Caption(1.0, 2.5, "↓", fade_in=True, fade_out=True), "/tmp/0.txt", "/fonts/mono.ttf")

        self.assertIn("textfile='/tmp/0.txt'", filter_)
        self.assertIn("enable='between(t,1.000,2.500)'", filter_)
        self.assertIn("(t-1.000)", filter_)
        self.assertIn("(2.500-t)", filter_)

    def test_does_not_fade_between_captions(self) -> None:
        filter_ = drawtext(Caption(1.0, 2.5, "↓", fade_in=False, fade_out=False), "/tmp/0.txt", "/fonts/mono.ttf")

        self.assertIn("alpha='max(0,1)'", filter_)


if __name__ == "__main__":
    unittest.main()
