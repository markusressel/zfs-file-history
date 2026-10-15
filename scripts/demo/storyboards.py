"""The storyboards of the demo: one walks through the whole application (overview, the GIF of the README), the others
show one feature each (e.g. for release notes). See driver.py for how they are run, and Key for why each key says
what it shows, hides or is typed into.

The application runs in the directory "just demo" is started in (the repository), on the pools of the system: the
patterns wait for what the application shows, not for names of datasets or pools.
"""

from __future__ import annotations

from dataclasses import dataclass

from steps import (
    DOWN,
    ENTER,
    ESC,
    LEFT,
    RIGHT,
    SPACE,
    TAB,
    UP,
    Click,
    Key,
    Quit,
    Ready,
    Search,
    Step,
    Wait,
    Zoom,
    ZoomOut,
)

# how long the demo waits for an operation on a pool, e.g. until a scrub was started
ACTIVITY_TIMEOUT = 300.0

# an operation running on a pool, in the activity panel: its title, the pool and the progress, e.g. "Scrub  tank  31.8%"
WAIT_FOR_ACTIVITY = Wait(
    r"(Scrub|Resilver|TRIM|Device removal|RAIDZ expansion) +\S+ .*\d+(\.\d+)?%", timeout=ACTIVITY_TIMEOUT
)

# an alert of a pool (e.g. a device taken offline), in the alert bar above the activities of every page, waited for
# as long as for an activity, so it can be caused after the recording started
WAIT_FOR_ALERT = Wait(r"─ Alerts ─", timeout=ACTIVITY_TIMEOUT)
# a line of the alert bar: how severe, the pool (the group "at", which is clicked) and the problem
ALERT = r"(Warning|Error) +(?P<at>\S+) +\S"

# the tabs of the pages in the header while the page is shown, e.g. " 1 FILES     2  3  4 "
FILES_PAGE = r"1 FILES +2  3  4"
DATASETS_PAGE = r"1  2 DATASETS +3  4"
POOLS_PAGE = r"1  2  3 POOLS +4"
DISKS_PAGE = r"1  2  3  4 DISKS"


def tab(number: int) -> str:
    """Returns the pattern of the number of a page in the header that can be clicked (not the shown one), for Click
    on line 0."""
    return rf"(?<= )(?P<at>{number})(?=  | *$)"


# waits for the files and their snapshots, with which every storyboard starts
LOADED = [Wait(FILES_PAGE), Wait(r"\d+ snapshots")]
# the bottom border of the snapshots while only those in which the selection changed are shown (v)
ONLY_CHANGES = r"═ \d+ of \d+ snapshots ╝"


@dataclass(frozen=True)
class Storyboard:
    """A recording of the demo: the steps, the size of the terminal (columns, lines; smaller ones are easier to read
    in release notes), whether it needs an operation running on a pool (see WAIT_FOR_ACTIVITY), and whether it needs
    an alert of a pool (see WAIT_FOR_ALERT)."""

    name: str
    description: str
    steps: list[Step]
    size: tuple[int, int] = (140, 36)
    needs_activity: bool = False
    needs_alert: bool = False


OVERVIEW = Storyboard(
    name="overview",
    description="the whole application, for the README (screenshots/demo.gif)",
    size=(179, 42),
    steps=[
        *LOADED,
        # the activity panel is shown (a scrub can be started after the demo, the time until then is not in the GIF);
        # left out with --without-activity
        WAIT_FOR_ACTIVITY,
        Ready(),
        # through the files and the snapshots, then back to the files
        Key(DOWN, 3.0),
        Key(DOWN, 0.5),
        Key(DOWN, 0.5),
        Key(DOWN, 0.5),
        Key(TAB, 1.5),
        Key(DOWN, 0.6),
        Key(DOWN, 0.6),
        Key(DOWN, 0.6),
        Key(TAB, 1.5),
        # search for the file until it is the only entry left, so the history is the one of README.md
        Key("/", 1.0, shows=r"Filter: "),
        Search("README.md", until=r"\b1 of \d+ entries", into=r"Filter: ", delay=0.4),
        # ends typing, the filter stays (if it did not, "h" is typed into it, and the history is not shown)
        Key(ENTER, 1.0, into=r"Filter: "),
        Key("h", 1.0, shows=r"History of 'README\.md'"),
        # down the snapshots of the history, slow enough to see what changed in each; more presses than snapshots
        # only select the last one longer
        *[Key(DOWN, 2.0 if i == 0 else 1.5, into=r"History of 'README\.md'") for i in range(5)],
        Key(ESC, 2.0, hides=r"History of"),
        # datasets, clicked in the header
        Click(tab(2), 1.5, shows=DATASETS_PAGE, line=0),
        Key(DOWN, 2.0),
        Key(DOWN, 0.6),
        Key(DOWN, 0.6),
        # pools and the actions of the selected pool: the dialog is only opened and closed, no action is chosen
        Click(tab(3), 2.0, shows=POOLS_PAGE, line=0),
        Key(ENTER, 4.0, shows=r"Select Action"),
        Key(ESC, 3.0, hides=r"Select Action"),
        # the command menu, searching for the scrub actions
        Key(":", 2.5, shows=r" Commands "),
        Key("s", 1.0, into=r" Commands "),
        Key("c", 0.3, into=r" Commands "),
        Key("r", 0.3, into=r" Commands "),
        Key(ESC, 2.5, hides=r" Commands "),
        # the disks of the pools, their temperature and health
        Click(tab(4), 3.0, shows=DISKS_PAGE, line=0),
        # back to the files, with its key
        Key("1", 1.5, shows=FILES_PAGE),
        Quit(3.0),
    ],
)

FILES = Storyboard(
    name="files",
    description="the files page: folders, the versions in the snapshots, only the changes, the overview",
    steps=[
        *LOADED,
        Ready(),
        # the selection is compared with its versions in the snapshots, the overview below says what changed
        Key(DOWN, 2.0),
        Key(DOWN, 1.2),
        Key(DOWN, 1.2),
        # into a folder and back
        Key("/", 1.5, shows=r"Filter: "),
        Search("internal", until=r"\b1 of \d+ entries", into=r"Filter: ", delay=0.3),
        Key(ENTER, 0.8, into=r"Filter: "),
        Key(RIGHT, 1.2, shows=r"Path: \S*/internal\b"),
        Key(DOWN, 1.5),
        Key(DOWN, 1.0),
        Key(DOWN, 1.0),
        Key(LEFT, 2.0, hides=r"Path: \S*/internal\b"),
        # the versions of the selection: all snapshots (in a fresh state), only those in which it changed, and all
        # again; the count in the bottom border of the snapshots (the overview also says "4 of 31 snapshots")
        Key(TAB, 2.0),
        Key(DOWN, 1.2),
        Key(DOWN, 1.2),
        Key("v", 1.5, shows=ONLY_CHANGES),
        Key("v", 3.0, hides=ONLY_CHANGES),
        Key(TAB, 1.5),
        # the overview below the files, hidden and shown again
        Key("o", 2.0, hides=r"─ Overview ─"),
        Key("o", 2.0, shows=r"─ Overview ─"),
        Quit(2.5),
    ],
)

DATASETS = Storyboard(
    name="datasets",
    description="the datasets page: the tree, the dataset in its pool, unmounted datasets, permissions, properties",
    steps=[
        *LOADED,
        Key("2", 0.0, shows=DATASETS_PAGE),
        Wait(r"\d+ (of \d+ )?datasets"),
        Ready(),
        # the info shows each dataset within its pool
        Key(DOWN, 2.0),
        Key(DOWN, 1.5),
        Key(DOWN, 1.5),
        Key(UP, 1.5),
        # the unmounted datasets (e.g. boot environments), shown and hidden again
        Key("u", 2.0, hides=r"unmounted hidden"),
        Key(DOWN, 1.5),
        Key("u", 2.0, shows=r"unmounted hidden"),
        # a flat list, and the tree again
        Key("t", 1.5, hides=r"Collapse/expand"),
        Key("t", 2.0, shows=r"Collapse/expand"),
        # the delegated permissions and the properties: only shown, nothing is changed
        Key("p", 1.5, shows=r"Permissions on "),
        Key(DOWN, 1.5),
        Key(DOWN, 1.0),
        Key(ESC, 2.0, hides=r"Permissions on "),
        Key("e", 1.5, shows=r"Properties of "),
        Key(DOWN, 1.5),
        Key(DOWN, 1.0),
        Key(ESC, 2.0, hides=r"Properties of "),
        Quit(2.0),
    ],
)

POOLS = Storyboard(
    name="pools",
    description="the pools page: the health and status of the pools, their devices, the actions",
    steps=[
        *LOADED,
        Key("3", 0.0, shows=POOLS_PAGE),
        Wait(r"\d+ pools?\b"),
        Ready(),
        Key(DOWN, 2.5),
        Key(UP, 2.5),
        # the status and the devices, with their temperature and SMART health
        Key(TAB, 2.0),
        Key(TAB, 1.5),
        Key(DOWN, 1.2),
        Key(DOWN, 1.2),
        Key(TAB, 2.0),
        # the actions of the pool: only opened, none is chosen
        Key(ENTER, 1.5, shows=r"Select Action"),
        Key(DOWN, 1.2, into=r"Select Action"),
        Key(DOWN, 1.0, into=r"Select Action"),
        Key(ESC, 2.0, hides=r"Select Action"),
        Quit(2.0),
    ],
)

DISKS = Storyboard(
    name="disks",
    description="the disks page: each disk of the pools with its health, grouped by type or pool, its SMART values",
    steps=[
        *LOADED,
        Key("4", 0.0, shows=DISKS_PAGE),
        Wait(r"\d+ disks"),
        Ready(),
        Key(DOWN, 2.5),
        Key(DOWN, 2.0),
        Key(UP, 2.0),
        # grouped by pool, not grouped, and by type (hard disks, SSDs) again
        Key("b", 2.0, hides=r"\bSSDs\b|Hard disks"),
        Key("b", 2.0, into=DISKS_PAGE),
        Key("b", 2.0, shows=r"\bSSDs\b|Hard disks"),
        # the SMART attributes of the selected disk
        Key(TAB, 2.0),
        Key(DOWN, 1.2),
        Key(DOWN, 1.0),
        Key(DOWN, 1.0),
        Key(TAB, 2.0),
        Quit(2.0),
    ],
)

COMMAND_MENU = Storyboard(
    name="command-menu",
    description="the command menu: the commands of the shown page, settings switched with Space, Sort by…",
    steps=[
        *LOADED,
        Key("2", 0.0, shows=DATASETS_PAGE),
        Wait(r"\d+ (of \d+ )?datasets"),
        Ready(),
        Key(":", 2.0, shows=r" Commands "),
        # zoomed in to the menu, the page around it stays visible
        Zoom(r" Commands ", 0.5, padding=2),
        # down to the setting, which is on in a fresh state (the selection starts at the first command)
        Key(DOWN, 2.0, into=r" Commands "),
        Key(DOWN, 0.8, into=r"● Hide unmounted datasets"),
        # switched off with Space, the menu stays open; the datasets behind it change
        Key(SPACE, 1.5, shows=r"○ Hide unmounted datasets"),
        Key(ESC, 2.5, hides=r" Commands "),
        # opened again, the setting is still selected: switched back on
        Key(":", 2.5, shows=r" Commands "),
        Key(SPACE, 2.0, shows=r"● Hide unmounted datasets"),
        # the commands to sort by a column are found by searching (or with "Sort by…")
        Key("s", 2.5, shows=r"Sort by Name", into=r" Commands ", typed=True),
        *[Key(key, 0.3, into=r" Commands ", typed=True) for key in "ort use"],
        Key(ENTER, 2.0, hides=r" Commands "),
        Wait(r"Used [↑↓]"),
        ZoomOut(0.5),
        Quit(3.0),
    ],
)

FILE_HISTORY = Storyboard(
    name="file-history",
    description="the history of a file: its versions in the snapshots and what changed in each",
    steps=[
        *LOADED,
        Ready(),
        Key("/", 1.5, shows=r"Filter: "),
        Search("README.md", until=r"\b1 of \d+ entries", into=r"Filter: ", delay=0.3),
        Key(ENTER, 0.8, into=r"Filter: "),
        Key("h", 1.0, shows=r"History of 'README\.md'"),
        *[Key(DOWN, 2.5 if i == 0 else 2.0, into=r"History of 'README\.md'") for i in range(5)],
        # what changed compared with the version before, not with the file now
        Key("d", 2.0, into=r"History of 'README\.md'"),
        Key(UP, 2.5, into=r"History of 'README\.md'"),
        Key(UP, 2.0, into=r"History of 'README\.md'"),
        Key(ESC, 3.0, hides=r"History of"),
        Quit(1.5),
    ],
)

ACTIVITY = Storyboard(
    name="activity",
    description="a scrub in the activity panel: its progress, the time left or the ETA (clicked), the pool's status",
    needs_activity=True,
    steps=[
        *LOADED,
        WAIT_FOR_ACTIVITY,
        Ready(),
        # the time left and the ETA, cycled by clicking them: only the time left, only the ETA, both
        Click(r"⧗", 2.5, hides=r"⚑"),
        Click(r"⧗", 2.5, shows=r"⚑"),
        Click(r"⚑", 2.5, shows=r"⧗"),
        # a click on the activity shows its pool, with the progress in its status
        Click(r"(?P<at>Scrub|Resilver|TRIM|Device removal|RAIDZ expansion) +\S+ ", 2.5, shows=POOLS_PAGE),
        Key(DOWN, 4.0),
        Key(UP, 2.0),
        Quit(3.0),
    ],
)

ALERTS = Storyboard(
    name="alerts",
    description="an alert of a pool: shown above every page while something is wrong, a click shows the pool",
    needs_alert=True,
    steps=[
        *LOADED,
        WAIT_FOR_ALERT,
        Ready(),
        # the pool of the alert on the pools page: its status says what is wrong, the devices which one
        Click(ALERT, 3.0, shows=POOLS_PAGE),
        Key(TAB, 2.5),
        Key(TAB, 2.0),
        Key(DOWN, 1.2),
        Key(DOWN, 1.2),
        Key(DOWN, 1.2),
        Quit(3.0),
    ],
)

# the dialog of the history of a pool, while it lists the groups or each record (b)
POOL_HISTORY = r"History of "
HISTORY_GROUPS = r"\d+ of \d+ groups"
HISTORY_RECORDS = r"\d+ of \d+ records"
# the records of an expanded group, below it
HISTORY_EXPANDED = r"[├└]─ "

POOL_HISTORY_STORYBOARD = Storyboard(
    name="pool-history",
    description="the history of a pool: what was done, grouped into scrubs and runs of commands, and each record",
    # wide enough for the summaries of the groups and their user
    size=(160, 40),
    steps=[
        *LOADED,
        Key("3", 0.0, shows=POOLS_PAGE),
        Wait(r"\d+ pools?\b"),
        Ready(),
        # the second pool, if there is one (the root pool usually has the more varied history)
        Key(DOWN, 2.0),
        Key("h", 1.5, shows=POOL_HISTORY),
        Wait(HISTORY_GROUPS),
        # the groups, newest first, the selected one in full below the list
        Key(DOWN, 2.5),
        Key(DOWN, 1.5),
        Key(UP, 1.5),
        Key(UP, 1.2),
        # its records, and collapsed again
        Key(ENTER, 1.5, shows=HISTORY_EXPANDED),
        Key(DOWN, 2.0),
        Key(DOWN, 0.8),
        Key(DOWN, 0.8),
        Key("-", 1.5, into=HISTORY_EXPANDED),
        Key(ENTER, 1.5, hides=HISTORY_EXPANDED),
        # the scrubs: from their start to their end, how they ended
        Key("/", 2.0, into=POOL_HISTORY),
        Search("scrub", until=r"Filter: scrub\b", into=r"Filter: ", delay=0.3),
        Key(ENTER, 1.0, into=r"Filter: "),
        Key(DOWN, 1.5),
        Key(UP, 1.5),
        Key(ENTER, 1.5, shows=HISTORY_EXPANDED),
        # each record in the order they were logged, with the events ZFS logged itself, and the groups again
        Key("b", 3.0, shows=HISTORY_RECORDS),
        Key("i", 2.5, shows=r"hide events logged by ZFS"),
        Key("b", 3.0, shows=HISTORY_GROUPS),
        Key(ESC, 3.0, hides=POOL_HISTORY),
        Quit(1.5),
    ],
)

# the dialog of the features of a pool, and the confirmation of the chosen ones
POOL_FEATURES = r" Pool Features "
ENABLE_FEATURES = r" Enable Features "

POOL_FEATURES_STORYBOARD = Storyboard(
    name="pool-features",
    description="the features of a pool: chosen one by one, the others grayed out with the reason, confirmed by name",
    steps=[
        *LOADED,
        Key("3", 0.0, shows=POOLS_PAGE),
        Wait(r"\d+ pools?\b"),
        Ready(),
        # the actions of the first pool: "Enable features" is always the last one before Close, which the
        # selection stops at (the options of the pool differ, e.g. upgrade only while zpool suggests it)
        Key(ENTER, 2.0, shows=r"Select Action"),
        *[Key(DOWN, 0.4, into=r"Select Action") for _ in range(8)],
        Key(UP, 0.8, into=r"Select Action"),
        Key(ENTER, 1.5, shows=POOL_FEATURES),
        Zoom(POOL_FEATURES, 0.5, padding=2),
        # the description of each below the list; two of them checked
        Key(DOWN, 2.0, into=POOL_FEATURES),
        Key(SPACE, 1.5, into=POOL_FEATURES),
        Key(DOWN, 1.5, into=POOL_FEATURES),
        Key(SPACE, 1.2, into=POOL_FEATURES),
        # the ones that cannot be enabled: grayed out with the reason (enabled already, not allowed by the
        # compatibility of the pool, ...), they cannot be checked
        *[Key(DOWN, 0.5, into=POOL_FEATURES) for _ in range(14)],
        Key(DOWN, 2.5, into=POOL_FEATURES),
        # the confirmation asks for the name of the pool, which the demo never types: only shown and closed
        Key(ENTER, 2.0, shows=ENABLE_FEATURES),
        Key(ESC, 5.0, hides=ENABLE_FEATURES),
        ZoomOut(1.0),
        Quit(2.0),
    ],
)

STORYBOARDS: dict[str, Storyboard] = {
    storyboard.name: storyboard
    for storyboard in [
        OVERVIEW,
        FILES,
        DATASETS,
        POOLS,
        DISKS,
        COMMAND_MENU,
        FILE_HISTORY,
        POOL_HISTORY_STORYBOARD,
        POOL_FEATURES_STORYBOARD,
        ACTIVITY,
        ALERTS,
    ]
}
