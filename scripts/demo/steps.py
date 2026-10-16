"""The steps of a storyboard (see storyboards.py), run by the driver (see driver.py)."""

from __future__ import annotations

from dataclasses import dataclass

UP, DOWN, RIGHT, LEFT = "\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D"
TAB, ENTER, ESC, SPACE = "\t", "\r", "\x1b", " "
CTRL_Q = "\x11"

# keys that only move the selection or the focus, and can be typed without checking the screen
NAVIGATION_KEYS = {UP, DOWN, RIGHT, LEFT, TAB}

# how the GIF shows the keys (see render.captions), other keys as typed; in the font of the overlay, which has no ⭾
KEY_LABELS = {
    UP: "↑",
    DOWN: "↓",
    RIGHT: "→",
    LEFT: "←",
    TAB: "Tab",
    ENTER: "Enter",
    ESC: "Esc",
    SPACE: "Space",
    CTRL_Q: "ctrl+q",
}
# how the GIF shows a click (see Click)
CLICK_LABEL = "Click"


def key_label(keys: str) -> str:
    """Returns how the GIF shows the keys, e.g. "↓" or "v"."""
    return KEY_LABELS.get(keys, keys)


@dataclass(frozen=True)
class Key:
    """A key of the demo, typed after waiting delay seconds.

    shows: a pattern that must not be on the screen before the key, and must be after it (e.g. a dialog it opens).
    hides: a pattern that must be on the screen before the key, and must not be after it (e.g. a dialog it closes).
    into: a pattern that must be on the screen when the key is typed (e.g. the menu the text is typed into).
    typed: the key is a character of text (e.g. a search), the GIF shows the characters typed one after the other as
    one word.
    """

    keys: str
    delay: float = 0.0
    shows: str | None = None
    hides: str | None = None
    into: str | None = None
    typed: bool = False


@dataclass(frozen=True)
class Click:
    """A click with the mouse on what the pattern finds on the screen, after waiting delay seconds: the mouse is moved
    there first (so it is highlighted while hovered, for hover seconds), then pressed and released. It clicks the
    first cell of the match, or of its group "at" (e.g. r"(?P<at>3) +4$" for the 3 before the 4). line restricts
    the search to a line of the screen (e.g. 0 for the header). shows and hides as for Key."""

    pattern: str
    delay: float = 0.0
    shows: str | None = None
    hides: str | None = None
    line: int | None = None
    hover: float = 0.6


@dataclass(frozen=True)
class Select:
    """Moves the selection with key (a navigation key, ↓ by default) until the line the pattern finds is selected,
    i.e. drawn on the background of the selection (see driver.SELECTED_BACKGROUND), delay seconds before each key:
    for an entry whose position differs between systems, e.g. an action offered only for some pools. into as for
    Key. Stops the demo if it is not selected after max_keys keys, or if the selection does not move."""

    pattern: str
    into: str
    key: str = DOWN
    delay: float = 0.0
    max_keys: int = 20


@dataclass(frozen=True)
class Wait:
    """Waits until the pattern is on the screen, e.g. until the application has loaded, at most timeout seconds
    (WAIT_TIMEOUT if None)."""

    pattern: str
    timeout: float | None = None


@dataclass(frozen=True)
class Ready:
    """Waits until the application wrote nothing for quiet seconds (e.g. its layout settled after loading), and marks
    the recording there: prepare_cast drops what was shown before, so the GIF starts with the application ready."""

    quiet: float = 0.5


@dataclass(frozen=True)
class Quit:
    """Quits the application (ctrl+q) after waiting delay seconds, and waits until it has quit."""

    delay: float = 0.0


@dataclass(frozen=True)
class Search:
    """Types text into a search field one character at a time, delay seconds apart, until the pattern until is on the
    screen (e.g. only one entry is left). into is a pattern for the field, e.g. its label: after each character, the
    driver waits until the field shows the text typed so far, so it knows that the character arrived. Stops the demo
    if until is not shown after the whole text."""

    text: str
    until: str
    into: str
    delay: float = 0.0


@dataclass(frozen=True)
class Zoom:
    """Zooms the GIF in to the box (a pane or dialog with a border) whose top border shows the pattern, e.g. the title
    r" Commands ", with padding cells around it, so the rest of the application stays visible as context. The driver
    finds the box on the screen and marks the recording (see prepare_cast.ZOOM_MARKER); render.py zooms in from there,
    smoothly, until ZoomOut. Waits delay seconds first."""

    title: str
    delay: float = 0.0
    padding: int = 3


@dataclass(frozen=True)
class ZoomOut:
    """Zooms the GIF out again (see Zoom), after waiting delay seconds."""

    delay: float = 0.0


Step = Key | Click | Select | Wait | Ready | Search | Quit | Zoom | ZoomOut
