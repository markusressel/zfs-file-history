"""Tests of preparing the recording for agg: "just demo-test"."""

import unittest

from prepare_cast import (
    END_OF_FRAME,
    LEAVE_ALTERNATE_SCREEN,
    NBSP,
    READY_MARKER,
    ZOOM_OUT_MARKER,
    extract_markers,
    key_marker,
    prepare_events,
    zoom_marker,
)

FRAME = "\x1b[?2026hframe" + END_OF_FRAME
TEARDOWN = "\x1b[?25h\x1b[H\x1b[2J"


class PrepareEventsTest(unittest.TestCase):
    def test_replaces_non_breaking_spaces_in_the_output(self) -> None:
        events = prepare_events([[0.1, "o", f"Go{NBSP}to"], [0.2, "i", f"in{NBSP}put"]])

        self.assertEqual([[0.1, "o", "Go to"], [0.2, "i", f"in{NBSP}put"]], events)

    def test_drops_everything_after_the_last_frame(self) -> None:
        # the teardown and leaving the alternate screen in the same event as the last frame
        events = prepare_events(
            [[0.1, "o", FRAME], [0.2, "o", FRAME + TEARDOWN + LEAVE_ALTERNATE_SCREEN + "\x1b[?1000l"]]
        )

        self.assertEqual([[0.1, "o", FRAME], [0.2, "o", FRAME]], events)

    def test_drops_the_teardown_in_the_events_before_leaving_the_alternate_screen(self) -> None:
        # Regression: the screen was cleared in an event before the one leaving the alternate screen, and the GIF
        # ended with an empty terminal
        events = prepare_events(
            [
                [0.1, "o", FRAME],
                [0.2, "o", FRAME + "\x1b[?25h"],
                [0.3, "o", TEARDOWN],
                [0.0, "o", LEAVE_ALTERNATE_SCREEN + "\x1b[?1000l"],
                [0.1, "x", "0"],
            ]
        )

        self.assertEqual([[0.1, "o", FRAME], [0.2, "o", FRAME]], events)

    def test_starts_with_the_ready_application(self) -> None:
        events = prepare_events(
            [
                [0.1, "o", "\x1b[?1049h"],
                # a key typed before the application was ready (e.g. to another page) is not in the GIF
                [0.3, "o", key_marker("2") + "loading"],
                [0.5, "o", FRAME + key_marker("3") + READY_MARKER + key_marker("4") + "rest"],
                [2.0, "o", FRAME],
            ]
        )

        self.assertEqual(
            [
                [0.0, "o", "\x1b[?1049h"],
                [0.0, "o", "loading"],
                [0.0, "o", FRAME],
                [0.0, "o", key_marker("4") + "rest"],
                [2.0, "o", FRAME],
            ],
            events,
        )

    def test_keeps_a_recording_in_which_the_application_did_not_quit(self) -> None:
        events = [[0.1, "o", FRAME], [0.2, "o", FRAME]]

        self.assertEqual(events, prepare_events(events))

    def test_drops_only_what_follows_without_a_finished_frame(self) -> None:
        events = prepare_events([[0.1, "o", "text"], [0.2, "o", "more" + LEAVE_ALTERNATE_SCREEN + "after"]])

        self.assertEqual([[0.1, "o", "text"], [0.2, "o", "more"]], events)


class ExtractMarkersTest(unittest.TestCase):
    def test_removes_the_markers_and_returns_the_zooms_at_their_times_in_the_gif(self) -> None:
        events, zooms, _ = extract_markers(
            [
                [0.0, "o", "start"],
                # a pause longer than agg's idle time limit counts with the limit
                [5.0, "o", "a" + zoom_marker(10, 5, 30, 12, 2) + "b"],
                [1.5, "o", ZOOM_OUT_MARKER],
                [0.5, "i", "key"],
            ],
            columns=40,
            lines=20,
        )

        self.assertEqual([[0.0, "o", "start"], [5.0, "o", "ab"], [1.5, "o", ""], [0.5, "i", "key"]], events)
        self.assertEqual([{"start": 2.0, "end": 3.5, "left": 8, "top": 3, "right": 32, "bottom": 14}], zooms)

    def test_keeps_the_padding_within_the_screen_and_ends_an_open_zoom_with_the_recording(self) -> None:
        _, zooms, _ = extract_markers(
            [[1.0, "o", zoom_marker(1, 0, 39, 19, 3)], [1.0, "o", "end"]], columns=40, lines=20
        )

        self.assertEqual([{"start": 1.0, "end": 2.0, "left": 0, "top": 0, "right": 39, "bottom": 19}], zooms)

    def test_removes_the_key_markers_and_returns_the_keys_at_their_times_in_the_gif(self) -> None:
        events, _, keys = extract_markers(
            [
                [1.0, "o", key_marker("↓") + "frame"],
                # characters that would end the marker or a percent sign arrive as typed
                [3.0, "o", key_marker(";%\x07", typed=True)],
            ],
            columns=40,
            lines=20,
        )

        self.assertEqual([[1.0, "o", "frame"], [3.0, "o", ""]], events)
        self.assertEqual(
            [{"time": 1.0, "label": "↓", "typed": False}, {"time": 3.0, "label": ";%\x07", "typed": True}], keys
        )


if __name__ == "__main__":
    unittest.main()
