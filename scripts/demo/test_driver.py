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
from driver import CTRL_Q, DOWN, ENTER, ESC, DemoFailed, Key, Quit, Ready, Search, Wait
from prepare_cast import READY_MARKER

# A fake application: shows "MAIN", Enter opens "DIALOG", Escape closes it, ctrl+q quits. "/" starts a search in
# ENTRIES (shown as "Filter: <text>" and "<n> of 3 entries"), which Enter ends. Every key it receives is appended to
# the file in $KEYS, so the tests can tell which keys arrived, and its environment is written to the file in $ENV.
FAKE_APP = """\
    #!/usr/bin/env python3
    import json, os, sys, termios, tty
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
        if search is not None and key == "\\r":
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
    def test_the_demo_is_safe_to_run(self) -> None:
        driver.check_script(driver.SCRIPT)

    def test_refuses_keys_without_a_condition(self) -> None:
        for keys in [ENTER, ESC, "h", "1", ":"]:
            with self.subTest(keys=keys), self.assertRaisesRegex(DemoFailed, "must say what it shows"):
                driver.check_script([Wait("MAIN"), Key(keys)])

    def test_allows_navigation_keys_without_a_condition(self) -> None:
        driver.check_script([Wait("MAIN")] + [Key(key) for key in driver.NAVIGATION_KEYS])

    def test_refuses_scripts_that_do_not_wait_for_the_application_first(self) -> None:
        for script in [[], [Key(DOWN), Wait("MAIN")]]:
            with self.subTest(script=script), self.assertRaisesRegex(DemoFailed, "must start with a Wait"):
                driver.check_script(script)

    def test_waits_for_the_activity_panel(self) -> None:
        pattern = re.compile(driver.WAIT_FOR_ACTIVITY.pattern)
        for line in [
            " Scrub  rpool  [ 31.8%  1.02T / 3.21T  294M/s        ]   ⧗    2h 10m ",
            " Resilver  tank  [ 5%  ]",
            " RAIDZ expansion  tank  [ 99.9% ]",
        ]:
            with self.subTest(line=line):
                self.assertIsNotNone(pattern.search(line))
        self.assertIsNone(pattern.search(" zfs-file-history   2.3.0   FILES    1/3 "))
        self.assertIn(driver.WAIT_FOR_ACTIVITY, driver.SCRIPT)

    def test_refuses_invalid_patterns(self) -> None:
        with self.assertRaisesRegex(DemoFailed, "invalid pattern"):
            driver.check_script([Wait("MAIN"), Key(ENTER, shows="(")])


if __name__ == "__main__":
    unittest.main()
