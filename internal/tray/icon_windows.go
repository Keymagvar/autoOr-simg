//go:build windows

package tray

import _ "embed"

// icon.ico: vendored from autoortho/autoortho/imgs/ao-icon.ico (the same
// icon Python's own Windows build already uses, via Nuitka's
// --windows-icon-from-ico). systray's Windows backend writes this to a
// temp .ico file and loads it via the Win32 icon APIs, which require a
// real ICO container — a plain PNG does not work here, unlike
// icon_other.go's platforms.
//
//go:embed icon.ico
var iconData []byte
