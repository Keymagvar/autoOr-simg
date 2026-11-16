// Package tray is the systray half of DESIGN.md's "Still open" list: a
// native taskbar/menu-bar icon with Open/Stop/Restart/Shutdown shortcuts,
// wrapping github.com/getlantern/systray (which itself wraps Windows
// Shell_NotifyIcon, macOS NSStatusItem, and Linux's AppIndicator/
// StatusNotifierItem protocol behind one API, per DESIGN.md's own
// reasoning for picking that library over hand-rolling each platform).
//
// The menu items call exactly the same functions the web UI's own
// Run/Stop/Restart/Shutdown buttons call (via Callbacks, wired in once
// they exist) — this package adds no new control-flow, just another way
// to reach the one that's already there.
package tray

import (
	"sync"

	"github.com/getlantern/systray"
)

// Callbacks are invoked on menu-item clicks. Shutdown must run the real
// guaranteed-unmount-then-exit sequence (DESIGN.md §4c/§8) — same as a
// SIGINT or the web UI's own Shutdown button, not a raw process exit.
// Restart is that same sequence followed immediately by a fresh instance —
// same as the web UI's own Restart button (Server.RequestRestart).
type Callbacks struct {
	Open     func()
	Stop     func()
	Restart  func()
	Shutdown func()
}

var (
	mu        sync.Mutex
	cb        Callbacks
	mOpen     *systray.MenuItem
	mStop     *systray.MenuItem
	mRestart  *systray.MenuItem
	mShutdown *systray.MenuItem
)

// Run blocks until Quit is called. It MUST be called directly from main()
// (not from a spawned goroutine): systray locks the OS thread that first
// touches it (see its own init()), which on Windows/macOS has to be the
// thread holding the native GUI event loop, i.e. the one main() started
// on. fn is launched in its own goroutine once the tray icon exists —
// typically the real "run" command's actual startup/mount/serve logic —
// and must call Quit() itself once it has finished its own shutdown
// sequence, so this call (and the process) can exit normally afterward.
func Run(fn func()) {
	systray.Run(func() {
		onReady()
		go fn()
	}, func() {})
}

// Quit stops the tray's native event loop, letting Run return. Safe to
// call more than once or from any goroutine.
func Quit() {
	systray.Quit()
}

func onReady() {
	if len(iconData) > 0 {
		systray.SetIcon(iconData)
	}
	systray.SetTitle("autoOr-simg")
	systray.SetTooltip("autoOr-simg")

	mOpen = systray.AddMenuItem("Open autoOr-simg", "Open the web UI in your browser")
	systray.AddSeparator()
	mStop = systray.AddMenuItem("Stop", "Unmount scenery, keep running")
	mRestart = systray.AddMenuItem("Restart", "Unmount everything, then start a fresh instance")
	mShutdown = systray.AddMenuItem("Shutdown", "Unmount everything and exit")

	// Disabled until SetCallbacks is called: the web server/engine these
	// actions need don't exist yet during the first moment of startup
	// (mounting scenery happens before the web UI comes up), and a click
	// with nothing wired up would silently do nothing anyway — disabling
	// makes that visible instead of just inert.
	mOpen.Disable()
	mStop.Disable()
	mRestart.Disable()
	mShutdown.Disable()

	go dispatch()
}

func dispatch() {
	for {
		select {
		case <-mOpen.ClickedCh:
			invoke(func() func() { mu.Lock(); defer mu.Unlock(); return cb.Open }())
		case <-mStop.ClickedCh:
			invoke(func() func() { mu.Lock(); defer mu.Unlock(); return cb.Stop }())
		case <-mRestart.ClickedCh:
			invoke(func() func() { mu.Lock(); defer mu.Unlock(); return cb.Restart }())
		case <-mShutdown.ClickedCh:
			invoke(func() func() { mu.Lock(); defer mu.Unlock(); return cb.Shutdown }())
		}
	}
}

func invoke(f func()) {
	if f != nil {
		f()
	}
}

// SetCallbacks wires the menu items to real actions and enables them —
// call once the web server/engine they act on actually exist (runCmd,
// right after webui.New()). Safe to call from any goroutine.
func SetCallbacks(c Callbacks) {
	mu.Lock()
	cb = c
	mu.Unlock()

	mOpen.Enable()
	mStop.Enable()
	mRestart.Enable()
	mShutdown.Enable()
}
