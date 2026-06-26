#### 1. Focus Management ( ShowDialogOnPages )

Modified  ShowDialogOnPages  in util.go to include  *tview.TextView  and  *tview.TextArea  in the focus-management switch:

• When clicking on the "Content" view, it is now correctly classified as a valid focusable leaf.
• Focus is no longer automatically reverted back to the table, allowing mouse interaction to work in addition to the  Tab  keyboard toggle.

#### 2. Copy to Clipboard Utility

Created a platform-independent clipboard copying function in clipboard.go:

• Automatically attempts to use popular clipboard utilities ( wl-copy, xclip, xsel, pbcopy,  clip.exe ).
• Exposes clean errors when no utility is found.

#### 3. Keyboard Copy Shortcut and Feedback

Wired up the clipboard actions in file_history_overlay.go:

• Raw Diff Capture: Added a  currentRawDiff  field that records the raw unified diff text prior to inserting color formatting tags (like  [green]  and  [red] ),
ensuring clean copies without formatting markup.
• Copy Shortcut: Registered  c / C  keybindings on both the snapshot list table and the diff view.
• Premium Feedback: Added a dynamic  copyShortcutLabel.When a user copies successfully, the shortcut bar at the bottom dynamically changes from  [c]: Copy diff  to
[c]: Copied!  for 2 seconds, before returning to normal.
• Error Dialog: If copying fails (e.g.no clipboard utility is installed), a descriptive error dialog is shown suggesting tools like  wl-clipboard, xclip, or
xsel .
• Mouse Drag Selection: Note that because mouse support is globally enabled, standard mouse drags are handled by  tview.Users can visually select and copy
arbitrary text blocks from the diff by holding down  Shift  while dragging the mouse (a standard feature of terminal emulators like Alacritty or GNOME Terminal).
