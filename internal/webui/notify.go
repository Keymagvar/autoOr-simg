package webui

import (
	"fmt"

	"github.com/gen2brain/beeep"
	"github.com/pkg/browser"
)

// NotifyStartup fires one native OS notification (Windows toast, macOS
// Notification Center, Linux D-Bus/notify-send via beeep) pointing at the
// running page — DESIGN.md §3's replacement for config_ui.py's splash/main
// window: "instead of opening any window... fire one native OS
// notification."
func NotifyStartup(url string) error {
	return beeep.Notify("autoOr-simg", fmt.Sprintf("autoOr-simg is running — configure it at %s", url), "")
}

// OpenBrowser opens the user's default browser at url — paired with
// NotifyStartup, gated by config.General.AutoOpenUI (default true) per
// DESIGN.md §3.
func OpenBrowser(url string) error {
	return browser.OpenURL(url)
}
