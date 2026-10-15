"""Tests of the demo driver, against small fake applications instead of zfs-file-history: "just demo-test"."""

import io
import json
import os
import re
import stat
import tempfile
import textwrap
import unittest

import driver
from driver import DemoFailed
from prepare_cast import KEY_MARKER_PATTERN, READY_MARKER
from steps import CTRL_Q, DOWN, ENTER, ESC, Click, Key, Quit, Ready, Search, Wait
from storyboards import (
    ACTIVITY,
    DATASETS_PAGE,
    DISKS_PAGE,
    FILES_PAGE,
    OVERVIEW,
    POOLS_PAGE,
    STORYBOARDS,
    WAIT_FOR_ACTIVITY,
    tab,
)

# the header of the application, with the tabs of the pages at its end
HEADER = " zfs-file-history   2.5.0-(#4fc37df)-2026-10-14   {tabs}"

# A fake application: shows "MAIN", Enter opens "DIALOG", Escape closes it, ctrl+q quits. "/" starts a search in
# ENTRIES (shown as "Filter: <text>" and "<n> of 3 entries"), which Enter ends. A press of the left mouse button shows
# "CLICKED <column>;<line>" (1-based, as sent). Every key it receives is appended to
# the file in $KEYS, so the tests can tell which keys arrived, and its environment is written to the file in $ENV.
FAKE_APP = """\
    #!/usr/bin/env python3
    import json, os, re, sys, termios, tty
    ENTRIES = ["README.md", "screenshots", "main.go"]
    with open(os.environ["ENV"], "w") as env:
        json.dump(dict(os.environ), env)
    tty.setraw(0)
    def show(text):
        sys.stdout.write("\\x1b[2J\\x1b[H" + text)
        sys.stdout.flush()
    def show_search(text):
        matches = [entry for entry in ENTRIES if text.lower() in entry.lower()]
        show(f"MAIN Filter: {text}\\r\\n{len(matches)} of {len(ENTRIES)} entries")
    show("MAIN")
    search = None
    while True:
        key = os.read(0, 16).decode()
        with open(os.environ["KEYS"], "a") as keys:
            keys.write(repr(key) + "\\n")
        press = re.search(r"\\x1b\\[<0;(\\d+);(\\d+)M", key)
        if press:
            show(f"MAIN CLICKED {press[1]};{press[2]}")
        elif search is not None and key == "\\r":
            search = None
        elif search is not None:
            search += key
            show_search(search)
        elif key == "/":
            search = ""
            show_search(search)
        elif key == "\\r":
            show("MAIN DIALOG")
        elif key == "\\x1b":
            show("MAIN")
        elif key == "\\x11":
            break
"""


class DriverTest(unittest.TestCase):
    def setUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.app = self.write_app(FAKE_APP)
        self.keys = os.path.join(self.directory.name, "keys")
        self.error_file = os.path.join(self.directory.name, "error")
        self.env = os.path.join(self.directory.name, "env")
        for name, value in [("KEYS", self.keys), ("ENV", self.env)]:
            self.addCleanup(os.environ.pop, name, None)
            os.environ[name] = value
        original_timeout = driver.WAIT_TIMEOUT
        driver.WAIT_TIMEOUT = 1.0
        self.addCleanup(setattr, driver, "WAIT_TIMEOUT", original_timeout)

    def write_app(self, source: str) -> str:
        path = os.path.join(self.directory.name, "app")
        with open(path, "w") as file:
            file.write(textwrap.dedent(source))
        os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR)
        return path

    def run_script(self, script: list) -> bool:
        self.output = io.BytesIO()
        return driver.run_demo(self.app, self.directory.name, self.error_file, script, self.output)

    def received_keys(self) -> list[str]:
        if not os.path.exists(self.keys):
            return []
        with open(self.keys) as file:
            return [eval(line) for line in file]

    def error(self) -> str:
        with open(self.error_file) as file:
            return file.read()

    def test_runs_the_script(self) -> None:
        ok = self.run_script(
            [
                Wait("MAIN"),
                Key(DOWN),
                Key(ENTER, shows="DIALOG"),
                Key(ESC, hides="DIALOG"),
                Quit(),
            ]
        )

        self.assertTrue(ok)
        self.assertFalse(os.path.exists(self.error_file))
        self.assertEqual([DOWN, ENTER, ESC, CTRL_Q], self.received_keys())

    def test_clicks_what_the_pattern_finds(self) -> None:
        ok = self.run_script([Wait("MAIN"), Click(r"M(?P<at>A)IN", shows=r"CLICKED 2;1", hover=0.1), Quit()])

        self.assertTrue(ok, self.error() if os.path.exists(self.error_file) else "")
        received = "".join(self.received_keys())
        self.assertIn("\x1b[<35;2;1M", received, "hovered first")
        self.assertIn("\x1b[<0;2;1M\x1b[<0;2;1m", received.replace("'", ""), "pressed and released")

    def test_stops_if_there_is_nothing_to_click(self) -> None:
        ok = self.run_script([Wait("MAIN"), Click(r"NOTHING", shows=r"CLICKED"), Quit()])

        self.assertFalse(ok)
        self.assertIn("/NOTHING/ to click is not shown", self.error())

    def test_starts_the_application_with_an_empty_state(self) -> None:
        self.assertTrue(self.run_script([Wait("MAIN"), Quit()]))

        with open(self.env) as file:
            state_home = json.load(file)["XDG_STATE_HOME"]
        self.assertNotEqual(os.environ.get("XDG_STATE_HOME"), state_home)
        self.assertFalse(os.path.exists(state_home), "removed after the demo")

    def test_searches_until_the_pattern_is_shown(self) -> None:
        ok = self.run_script(
            [
                Wait("MAIN"),
                Key("/", shows="Filter: "),
                Search("README.md", until=r"\b1 of \d+ entries", into="Filter: "),
                Key(ENTER, into="Filter: "),
                # apart, so the fake application does not read both keys at once
                Quit(0.2),
            ]
        )

        self.assertTrue(ok)
        # "re" also matches "screenshots"
        self.assertEqual(["/", "R", "E", "A", ENTER, CTRL_Q], self.received_keys())

    def test_stops_if_the_search_does_not_find_it(self) -> None:
        ok = self.run_script(
            [
                Wait("MAIN"),
                Key("/", shows="Filter: "),
                Search("xy", until=r"\b1 of", into="Filter: "),
                Key(ESC, hides="x"),
            ]
        )

        self.assertFalse(ok)
        self.assertIn("/\\b1 of/ was not shown after typing 'xy'", self.error())
        self.assertEqual(["/", "x", "y"], self.received_keys())

    def test_does_not_search_without_the_search_field(self) -> None:
        ok = self.run_script([Wait("MAIN"), Search("README.md", until=r"\b1 of", into="Filter: ")])

        self.assertFalse(ok)
        self.assertIn("/Filter: / must be shown before typing 'R'", self.error())
        self.assertEqual([], self.received_keys())

    def test_waits_as_long_as_the_step_says(self) -> None:
        self.app = self.write_app("""\
            #!/usr/bin/env python3
            import time
            print("MAIN", flush=True)
            time.sleep(1.5)
            print("LATER", flush=True)
            time.sleep(60)
        """)

        # WAIT_TIMEOUT is 1 s in these tests
        ok = self.run_script([Wait("MAIN"), Wait("LATER", timeout=5.0), Wait("never", timeout=0.2)])

        self.assertFalse(ok)
        self.assertIn("/never/ did not show up within 0.2 s", self.error())

    def test_marks_when_the_application_is_ready(self) -> None:
        self.assertTrue(self.run_script([Wait("MAIN"), Ready(quiet=0.2), Key(ENTER, shows="DIALOG"), Quit()]))

        output = self.output.getvalue().decode()
        self.assertEqual(1, output.count(READY_MARKER))
        before, after = output.split(READY_MARKER)
        self.assertIn("MAIN", before)
        self.assertIn("DIALOG", after)

    def test_marks_the_keys_and_clicks_but_not_quitting(self) -> None:
        ok = self.run_script(
            [
                Wait("MAIN"),
                Key(DOWN),
                Click(r"M(?P<at>A)IN", shows=r"CLICKED 2;1", hover=0.1),
                Key(ENTER, shows="DIALOG"),
                Key(" ", typed=True, into="DIALOG"),
                Quit(),
            ]
        )

        self.assertTrue(ok, self.error() if os.path.exists(self.error_file) else "")
        marked = [(kind, label) for kind, label in KEY_MARKER_PATTERN.findall(self.output.getvalue().decode())]
        self.assertEqual([("key", "%E2%86%93"), ("key", "Click"), ("key", "Enter"), ("text", "%20")], marked)

    def test_stops_if_the_application_does_not_stop_drawing(self) -> None:
        self.app = self.write_app("""\
            #!/usr/bin/env python3
            import time
            while True:
                print("MAIN", flush=True)
                time.sleep(0.05)
        """)

        ok = self.run_script([Wait("MAIN"), Ready(quiet=0.2)])

        self.assertFalse(ok)
        self.assertIn("did not stop drawing", self.error())
        self.assertNotIn(READY_MARKER, self.output.getvalue().decode())

    def test_stops_if_a_screen_does_not_show_up(self) -> None:
        ok = self.run_script([Wait("MAIN"), Key("x", shows="DIALOG"), Key(ENTER, shows="DIALOG")])

        self.assertFalse(ok)
        self.assertIn("/DIALOG/ did not show up", self.error())
        self.assertIn("MAIN", self.error(), "the last screen")
        self.assertEqual(["x"], self.received_keys(), "nothing is typed after the failed step")

    def test_stops_if_a_screen_does_not_go_away(self) -> None:
        ok = self.run_script(
            [Wait("MAIN"), Key(ENTER, shows="DIALOG"), Key("x", hides="DIALOG"), Key(ENTER, shows="other")]
        )

        self.assertFalse(ok)
        self.assertIn("/DIALOG/ did not go away", self.error())
        self.assertEqual([ENTER, "x"], self.received_keys())

    def test_does_not_type_a_key_whose_screen_is_already_shown(self) -> None:
        # the wait would pass at once, before the key had an effect
        ok = self.run_script([Wait("MAIN"), Key(ENTER, shows="MAIN")])

        self.assertFalse(ok)
        self.assertIn("/MAIN/ must not be shown before typing", self.error())
        self.assertEqual([], self.received_keys())

    def test_does_not_type_into_something_that_is_not_shown(self) -> None:
        ok = self.run_script([Wait("MAIN"), Key("s", into="DIALOG")])

        self.assertFalse(ok)
        self.assertIn("/DIALOG/ must be shown before typing 's'", self.error())
        self.assertEqual([], self.received_keys())

    def test_stops_an_application_that_does_not_quit(self) -> None:
        self.app = self.write_app("""\
            #!/usr/bin/env python3
            import signal, time
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            print("MAIN", flush=True)
            time.sleep(60)
        """)
        original_quit_timeout = driver.QUIT_TIMEOUT
        driver.QUIT_TIMEOUT = 0.5
        self.addCleanup(setattr, driver, "QUIT_TIMEOUT", original_quit_timeout)

        ok = self.run_script([Wait("MAIN"), Quit()])

        self.assertFalse(ok)
        self.assertIn("did not quit", self.error())

    def test_does_not_run_the_driver_twice_if_the_application_cannot_be_started(self) -> None:
        # the child of pty.fork must not return into the driver: two drivers would type into the same terminal
        self.app = os.path.join(self.directory.name, "missing")
        pid = os.getpid()

        ok = self.run_script([Wait("MAIN")])

        self.assertEqual(pid, os.getpid())
        self.assertFalse(ok)
        self.assertIn("/MAIN/ did not show up", self.error())


class CheckScriptTest(unittest.TestCase):
    def test_the_storyboards_are_safe_to_run(self) -> None:
        for storyboard in STORYBOARDS.values():
            for without_activity in [False, True]:
                with self.subTest(storyboard=storyboard.name, without_activity=without_activity):
                    driver.check_script(driver.storyboard_steps(storyboard, without_activity))

    def test_refuses_keys_without_a_condition(self) -> None:
        for keys in [ENTER, ESC, "h", "1", ":"]:
            with self.subTest(keys=keys), self.assertRaisesRegex(DemoFailed, "must say what it shows"):
                driver.check_script([Wait("MAIN"), Key(keys)])

    def test_refuses_clicks_without_a_condition(self) -> None:
        with self.assertRaisesRegex(DemoFailed, "must say what it shows or hides"):
            driver.check_script([Wait("MAIN"), Click("MAIN")])

    def test_allows_navigation_keys_without_a_condition(self) -> None:
        driver.check_script([Wait("MAIN")] + [Key(key) for key in driver.NAVIGATION_KEYS])

    def test_refuses_scripts_that_do_not_wait_for_the_application_first(self) -> None:
        for script in [[], [Key(DOWN), Wait("MAIN")]]:
            with self.subTest(script=script), self.assertRaisesRegex(DemoFailed, "must start with a Wait"):
                driver.check_script(script)

    def test_refuses_invalid_patterns(self) -> None:
        for step in [Key(ENTER, shows="("), Click("(", shows="MAIN")]:
            with self.subTest(step=step), self.assertRaisesRegex(DemoFailed, "invalid pattern"):
                driver.check_script([Wait("MAIN"), step])


class StoryboardTest(unittest.TestCase):
    def test_waits_for_the_activity_panel(self) -> None:
        pattern = re.compile(WAIT_FOR_ACTIVITY.pattern)
        for line in [
            " Scrub  rpool  [ 31.8%  1.02T / 3.21T  294M/s        ]   ⧗    2h 10m ",
            " Resilver  tank  [ 5%  ]",
            " RAIDZ expansion  tank  [ 99.9% ]",
        ]:
            with self.subTest(line=line):
                self.assertIsNotNone(pattern.search(line))
        self.assertIsNone(pattern.search(HEADER.format(tabs=" 1 FILES     2  3  4 ")))

    def test_without_activity_only_if_the_storyboard_is_not_about_it(self) -> None:
        self.assertIn(WAIT_FOR_ACTIVITY, OVERVIEW.steps)
        self.assertNotIn(WAIT_FOR_ACTIVITY, driver.storyboard_steps(OVERVIEW, without_activity=True))
        self.assertIn(WAIT_FOR_ACTIVITY, driver.storyboard_steps(ACTIVITY, without_activity=True))

    def test_the_pages_in_the_header(self) -> None:
        tabs = {
            FILES_PAGE: " 1 FILES     2  3  4 ",
            DATASETS_PAGE: " 1  2 DATASETS  3  4 ",
            POOLS_PAGE: " 1  2  3 POOLS     4 ",
            DISKS_PAGE: " 1  2  3  4 DISKS    ",
        }
        for page, shown in tabs.items():
            line = HEADER.format(tabs=shown)
            with self.subTest(page=page):
                for other in tabs:
                    self.assertEqual(other == page, re.search(other, line) is not None, other)
                # the numbers of the other pages can be clicked, not the version, nor the shown page
                for number in range(1, 5):
                    match = re.search(tab(number), line)
                    if f" {number}  " in shown or shown.rstrip().endswith(f" {number}"):
                        self.assertIsNotNone(match, number)
                        self.assertEqual(str(number), line[match.start("at")])
                    else:
                        self.assertIsNone(match, number)

    def test_finds_the_box_to_zoom_in_to(self) -> None:
        screen = driver.Driver(0, 30, 8, io.BytesIO())
        screen.stream.feed(
            "text\r\n  ╔══ Commands ══╗\r\n  ║ Filter       ║\r\n  ╚══════════════╝\r\n┌ Pane ┐\r\n└──────┘".encode()
        )
        self.assertEqual((2, 1, 17, 3), screen.box(r" Commands "))
        self.assertEqual((0, 4, 7, 5), screen.box(r" Pane "))
        self.assertIsNone(screen.box(r"Filter"), "not in a top border")
        self.assertIsNone(screen.box(r"missing"))

    def test_finds_the_column_after_wide_characters(self) -> None:
        screen = driver.Driver(0, 20, 2, io.BytesIO())
        screen.stream.feed("名前 x".encode())
        self.assertEqual((5, 0), screen.find(r"x", None))
        self.assertEqual((0, 0), screen.find(r"名", 0))
        self.assertIsNone(screen.find(r"x", 1))


if __name__ == "__main__":
    unittest.main()
