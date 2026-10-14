"""Tests of preparing the recording for agg: "just demo-test"."""

import unittest

from prepare_cast import END_OF_FRAME, LEAVE_ALTERNATE_SCREEN, NBSP, prepare_events

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

    def test_keeps_a_recording_in_which_the_application_did_not_quit(self) -> None:
        events = [[0.1, "o", FRAME], [0.2, "o", FRAME]]

        self.assertEqual(events, prepare_events(events))

    def test_drops_only_what_follows_without_a_finished_frame(self) -> None:
        events = prepare_events([[0.1, "o", "text"], [0.2, "o", "more" + LEAVE_ALTERNATE_SCREEN + "after"]])

        self.assertEqual([[0.1, "o", "text"], [0.2, "o", "more"]], events)


if __name__ == "__main__":
    unittest.main()
