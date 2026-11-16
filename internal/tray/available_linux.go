//go:build linux

package tray

import "os"

// Available reports whether a GUI session actually looks reachable.
// Confirmed the hard way: systray's Linux backend calls into GTK, and
// GTK's own init() aborts the whole process ("Gtk-WARNING: cannot open
// display") instead of returning a graceful error when neither $DISPLAY
// nor $WAYLAND_DISPLAY is set — exactly the case of a user running
// `autoortho run` without -H/--headless on a true headless box (SSH
// session, server, container). Checking this first and skipping the tray
// entirely in that case keeps that scenario a no-op instead of a crash,
// consistent with how the startup toast/browser-open already degrade
// (they just log a warning and continue) rather than taking the whole
// process down with them.
func Available() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}
