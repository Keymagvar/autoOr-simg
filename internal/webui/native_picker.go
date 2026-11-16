package webui

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// errPickerUnavailable means no native folder-picker tool could be found on
// this machine — the client falls back to the in-page directory browser
// (handleAPIBrowse) when it sees this.
var errPickerUnavailable = errors.New("no native folder picker available")

// pickFolderNative pops a real OS folder-picker dialog on the machine the
// server runs on and blocks until the user closes it. This only makes
// sense because the web UI is loopback-bound by design (§10): server and
// browser are the same machine, so a dialog the server spawns appears on
// the same screen the browser tab is open in. Returns ("", nil) if the
// user cancelled.
func pickFolderNative(startDir string) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return pickFolderLinux(startDir)
	case "windows":
		return pickFolderWindows(startDir)
	case "darwin":
		return pickFolderDarwin(startDir)
	default:
		return "", errPickerUnavailable
	}
}

// pickFolderLinux tries zenity (GNOME/most distros) then kdialog (KDE) —
// whichever is actually installed. Both print the chosen path to stdout
// and exit non-zero with no output when the user cancels.
func pickFolderLinux(startDir string) (string, error) {
	if path, err := exec.LookPath("zenity"); err == nil {
		out, err := exec.Command(path, "--file-selection", "--directory",
			"--title=Select folder", "--filename="+startDir+"/").Output()
		return cleanPickerOutput(out, err)
	}
	if path, err := exec.LookPath("kdialog"); err == nil {
		out, err := exec.Command(path, "--getexistingdirectory", startDir).Output()
		return cleanPickerOutput(out, err)
	}
	return "", errPickerUnavailable
}

// pickFolderWindows shells out to the built-in .NET FolderBrowserDialog via
// powershell — no extra install needed on any supported Windows version.
func pickFolderWindows(startDir string) (string, error) {
	psPath, err := exec.LookPath("powershell")
	if err != nil {
		return "", errPickerUnavailable
	}
	script := `Add-Type -AssemblyName System.Windows.Forms
$f = New-Object System.Windows.Forms.FolderBrowserDialog
$f.SelectedPath = '` + strings.ReplaceAll(startDir, "'", "''") + `'
if ($f.ShowDialog() -eq 'OK') { Write-Output $f.SelectedPath }`
	out, err := exec.Command(psPath, "-NoProfile", "-NonInteractive", "-Command", script).Output()
	return cleanPickerOutput(out, err)
}

// pickFolderDarwin uses AppleScript's "choose folder" — built into every
// macOS install, no extra dependency.
func pickFolderDarwin(startDir string) (string, error) {
	osaPath, err := exec.LookPath("osascript")
	if err != nil {
		return "", errPickerUnavailable
	}
	script := `POSIX path of (choose folder with prompt "Select folder" default location (POSIX file "` + startDir + `"))`
	out, err := exec.Command(osaPath, "-e", script).Output()
	path, err := cleanPickerOutput(out, err)
	return strings.TrimSuffix(path, "/"), err
}

// cleanPickerOutput turns a dialog tool's raw stdout into (path, nil) on a
// real pick, ("", nil) on cancel (nonzero exit, no usable output), or
// ("", err) for a genuine failure (tool present but errored some other way).
func cleanPickerOutput(out []byte, runErr error) (string, error) {
	path := strings.TrimSpace(string(out))
	if runErr != nil {
		if path == "" {
			return "", nil // cancelled
		}
		return "", runErr
	}
	return path, nil
}
