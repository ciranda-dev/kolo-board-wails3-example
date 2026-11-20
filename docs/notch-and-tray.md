# Notch and tray

The **notch** is the black cut-out at the top of some Mac screens, around the camera. The example shows the latest change of another member in a small window at the top of the screen, around the notch when there is one. The **tray** is the small icon area of the system: the menu bar on macOS, the notification area on Windows, the tray on Linux.

## Tray (macOS, Windows, Linux)

- The menu has Open Kolo Board, the status line (no board, joining, up to date, N changes since you looked, N syncing), the notch setting (macOS only), and Quit.
- The icon shows a dot while there are changes by other members that you have not seen. They count as seen when the window gets focus.
- Clicking the icon shows or hides the window.
- Closing the window quits the example.
- On Linux the tray is a D-Bus service: in a session without a D-Bus session bus the example runs without a tray icon and logs one line.

## Notch window (macOS only)

On a Mac, a second window made with Wails' `NewNotchWindow` shows the latest change by another member (who and what), how many cards are settled and syncing, and how many members are connected.

- It hangs from the top of the screen on every Mac; on a Mac with a notch, its shape follows the notch.
- It is hidden at start, opens when another member changes the board, and hides when you look at the board (the main window gets the focus) or hide it; with `-notch-linger` it also hides that long after the last change. A notch you opened yourself stays until you hide it.
- Cmd+Alt+Shift+B shows it again and keeps it open until you hide it. This keyboard shortcut exists on macOS only.
- On Windows and Linux the notch window does nothing.

## Turn it off

"Show changes in the notch", in Settings or in the menu bar menu, turns the notch window off. "Show board text in the notch", in Settings, keeps the notch but removes the board title, card text and column names from it: it then says who changed something, not what. Use it when other people can see your screen.

## What reaches the notch

The invite never reaches the notch page: it has no way to ask for one. The notch gets member names, counts, and, with the card text setting on, the title of the board and of the card that changed, within the same bounds as on the board; the page shows one line of each.

## Build tag

Wails draws the notch shape with a private macOS interface only when built with the tag `private_mac_apis`; the example is built without it, so the notch window's page is opaque but works.
