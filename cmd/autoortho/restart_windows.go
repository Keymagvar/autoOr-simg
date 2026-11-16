//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// DETACHED_PROCESS isn't exported by the standard syscall package on
// Windows — this is its documented raw value (Windows API CreateProcess
// flag), same constant golang.org/x/sys/windows defines under this name.
const detachedProcess = 0x00000008

// reExecSelf starts a fresh instance of this binary and exits the current
// process — Windows has no equivalent to Unix's exec() that replaces a
// process image in place, so this is a spawn-then-exit instead of a true
// re-exec (a new PID, not the same one). DETACHED_PROCESS so the child
// survives this process exiting right after (not a child of a process
// about to disappear, so no "parent died" cleanup takes it down too).
// Only returns if the spawn itself fails; on success this calls os.Exit
// directly rather than returning, since there is no "fall through to the
// normal shutdown print" that would make sense here — the new process is
// already running by the time this process needs to go away.
func reExecSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding own executable path: %w", err)
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawning replacement process: %w", err)
	}
	os.Exit(0)
	return nil // unreachable — os.Exit above never returns
}
