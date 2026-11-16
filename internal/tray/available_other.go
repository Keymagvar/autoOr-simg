//go:build !linux

package tray

// Available always reports true on Windows/macOS: unlike X11/Wayland,
// there's no equivalent cheap environment-variable check for "is a GUI
// session reachable" on those platforms, and a normal desktop login
// always has one (no real headless-server use case the way Linux has).
// See available_linux.go for the real, confirmed-by-crashing problem this
// guards against there.
func Available() bool {
	return true
}
