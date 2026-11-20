package main

import (
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"os"
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

//go:embed build/tray
var trayIcons embed.FS

const notchWindowName = "notch"

// The smallest main window the interface is designed for.
const (
	minWindowWidth  = 640
	minWindowHeight = 520
)

func main() {
	cfg, err := LoadConfig(os.Args[1:], os.Getenv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	applyMemoryLimit()
	svc := NewBoardService(cfg, nil)

	app := application.New(application.Options{
		Name:        "Kolo Board",
		Description: "A shared board for a team, built with Kolo",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: macOptions(),
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:  mainWindowName,
		Title: "Kolo Board",
		// Window sized to the golden ratio (1200 / 742 ≈ 1.617), as the other Kolo apps.
		Width:     1200,
		Height:    742,
		MinWidth:  minWindowWidth,
		MinHeight: minWindowHeight,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(7, 11, 18),
		URL:              "/",
	})
	// Events go to one window each: Event.Emit would send what only the main window may know to the notch page too.
	svc.emit = func(name string, data any) { win.DispatchWailsEvent(&application.CustomEvent{Name: name, Data: data}) }
	quitOnClose(win, app.Quit)
	// A focused main window shows the changes of other members: they count as seen.
	win.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) { svc.MarkSeen() })
	openApp := func() { win.Show().Focus() }

	// On systems without a notch the library returns an inert window, and Show and Hide do nothing.
	notch := app.Window.NewNotchWindow(withHousing(notchOptions(), notchAreaOf(primaryScreen())))
	svc.notch, svc.openApp = notchAdapter{notch}, openApp
	svc.emitNotch = func(name string, data any) {
		if w, ok := app.Window.GetByName(notchWindowName); ok {
			w.DispatchWailsEvent(&application.CustomEvent{Name: name, Data: data})
		}
	}
	if err := app.GlobalShortcut.Register(notchShortcut, func() { svc.NotchShow() }); err != nil {
		log.Printf("shortcut %s: %v", notchShortcut, err)
	}

	if trayUsable(runtime.GOOS, os.Getenv, dialUnix) {
		svc.tray = newTray(app, win, svc, openApp)
	} else {
		log.Print("no tray icon: this session has no D-Bus session bus")
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// notchOptions makes the notch window hidden at start: a change by another member shows it, and the shortcut.
func notchOptions() application.NotchWindowOptions {
	return application.NotchWindowOptions{
		Width:    notchWindowWidth,
		Height:   notchWindowH,
		Animated: true,
		WindowOptions: application.WebviewWindowOptions{
			Name:         notchWindowName,
			URL:          "/notch/",
			Hidden:       true,
			HideOnEscape: true,
		},
	}
}

type notchAdapter struct{ w *application.NotchWindow }

func (n notchAdapter) Show() { n.w.Show() }
func (n notchAdapter) Hide() { n.w.Hide() }

// wailsTray is the menu bar icon on macOS and the tray icon elsewhere, with its menu.
type wailsTray struct {
	tray       *application.SystemTray
	status, nt *application.MenuItem
	icon       []byte
	active     []byte
	template   bool
	menu       *application.Menu

	mu                   sync.Mutex // the menu's callbacks run on goroutines of their own
	lastActive, hasState bool
}

func newTray(app *application.App, win *application.WebviewWindow, svc *BoardService, openApp func()) *wailsTray {
	t := &wailsTray{tray: app.SystemTray.New()}
	t.icon, t.active, t.template = trayImages(runtime.GOOS)
	t.setIcon(false)
	t.tray.SetTooltip("Kolo Board")

	t.menu = app.NewMenu()
	t.menu.Add("Open Kolo Board").OnClick(func(*application.Context) { openApp() })
	t.status = t.menu.Add("No board").SetEnabled(false)
	if runtime.GOOS == "darwin" {
		t.menu.AddSeparator()
		t.nt = t.menu.AddCheckbox("Show changes in the notch", true).OnClick(func(ctx *application.Context) {
			if _, err := svc.SetShowInNotch(ctx.ClickedMenuItem().Checked()); err != nil {
				log.Print("notch setting: ", err)
			}
		})
	}
	t.menu.AddSeparator()
	t.menu.Add("Quit").OnClick(func(*application.Context) { app.Quit() })
	t.tray.SetMenu(t.menu)
	t.tray.OnClick(func() { trayClick(win, openApp) })
	return t
}

// set runs on the main thread, where the menu and the icon may be changed.
func (t *wailsTray) set(st TrayState) {
	application.InvokeAsync(func() {
		t.mu.Lock()
		changed := !t.hasState || st.Active != t.lastActive
		t.lastActive, t.hasState = st.Active, true
		t.mu.Unlock()
		t.status.SetLabel(st.Status)
		if t.nt != nil {
			t.nt.SetChecked(st.ShowInNotch)
		}
		if changed {
			t.setIcon(st.Active)
		}
	})
}

func (t *wailsTray) setIcon(active bool) {
	img := t.icon
	if active && len(t.active) > 0 {
		img = t.active
	}
	if t.template {
		t.tray.SetTemplateIcon(img)
	} else {
		t.tray.SetIcon(img)
	}
}

// trayImages picks the icons in build/tray for goos: the macOS template images, else the 32 px ones.
func trayImages(goos string) (icon, active []byte, template bool) {
	read := func(name string) []byte {
		b, _ := fs.ReadFile(trayIcons, "build/tray/"+name)
		return b
	}
	if goos == "darwin" {
		if icon = read("tray-template@2x.png"); icon != nil {
			return icon, read("tray-template-active@2x.png"), true
		}
	}
	return read("tray-32.png"), read("tray-active-32.png"), false
}
