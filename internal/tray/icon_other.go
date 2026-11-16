//go:build !windows

package tray

import _ "embed"

// icon.png: the same 60x60 app icon ao-icon.ico was rendered from
// (autoortho/autoortho/imgs/ao-icon.ico/.png in the Python original).
// Plain PNG, since that's universally decodable on Linux's
// GdkPixbuf-based tray backend and macOS's NSImage — unlike .ico, which
// Windows requires but isn't guaranteed to load cleanly on either of
// those (see icon_windows.go).
//
//go:embed icon.png
var iconData []byte
