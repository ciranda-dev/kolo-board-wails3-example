package main

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// macOptions keeps the app running while its windows are hidden. With ApplicationShouldTerminateAfterLastWindowClosed,
// AppKit ended the app when a click on the tray icon hid the main window, the only one on screen. Closing the main
// window with its red button still quits, through quitOnClose.
func macOptions() application.MacOptions { return application.MacOptions{} }

// shownWindow is the main window, as much of it as a click on the tray icon needs.
type shownWindow interface {
	IsVisible() bool
	Hide() application.Window
}

// trayClick hides the main window when it is on screen, and opens the app otherwise. Hide only takes the window
// off the screen: it does not close it, so the WindowClosing hook that quits never runs.
func trayClick(w shownWindow, openApp func()) {
	if w.IsVisible() {
		w.Hide()
		return
	}
	openApp()
}

// eventWindow is the main window, as much of it as quitOnClose needs.
type eventWindow interface {
	OnWindowEvent(events.WindowEventType, func(*application.WindowEvent)) func()
}

// quitOnClose makes the close button of the main window quit the app: the notch window and the tray outlive it.
func quitOnClose(w eventWindow, quit func()) {
	var once sync.Once
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { once.Do(quit) })
}
