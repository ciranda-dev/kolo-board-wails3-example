package main

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// fakeMainWindow records what the tray and the close button do to the main window.
type fakeMainWindow struct {
	visible bool
	calls   []string
	hooks   map[events.WindowEventType][]func(*application.WindowEvent)
}

func (w *fakeMainWindow) IsVisible() bool { return w.visible }
func (w *fakeMainWindow) Hide() application.Window {
	w.calls, w.visible = append(w.calls, "hide"), false
	return nil
}
func (w *fakeMainWindow) OnWindowEvent(e events.WindowEventType, f func(*application.WindowEvent)) func() {
	if w.hooks == nil {
		w.hooks = map[events.WindowEventType][]func(*application.WindowEvent){}
	}
	w.hooks[e] = append(w.hooks[e], f)
	return func() {}
}
func (w *fakeMainWindow) fire(e events.WindowEventType) {
	for _, f := range w.hooks[e] {
		f(nil)
	}
}

// A click on the tray icon hides the main window and the next one opens the app. Nothing on that path closes the
// window, and the app does not ask AppKit to end it when no window is left on screen (the cause of the app
// quitting on a click). The close button of the main window still quits, once.
func TestTrayClickTogglesAndNeverQuits(t *testing.T) {
	if macOptions().ApplicationShouldTerminateAfterLastWindowClosed {
		t.Fatal("hiding the main window from the tray would end the app")
	}
	w := &fakeMainWindow{visible: true}
	quits := 0
	quitOnClose(w, func() { quits++ })
	open := func() { w.calls, w.visible = append(w.calls, "open"), true }
	trayClick(w, open)
	trayClick(w, open)
	if strings.Join(w.calls, ",") != "hide,open" || !w.visible || quits != 0 {
		t.Fatalf("two clicks: %v, quits %d", w.calls, quits)
	}
	w.fire(events.Common.WindowClosing)
	w.fire(events.Common.WindowClosing)
	if quits != 1 {
		t.Fatalf("closing the main window quit %d times, want 1", quits)
	}
}
