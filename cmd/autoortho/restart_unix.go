//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// reExecSelf replaces this process's image with a fresh instance of the
// same binary, same args, same environment — syscall.Exec keeps the same
// PID (no parent/child relationship to manage, no window to lose track of
// the new process in). Only returns if the exec itself fails; on success
// this process is simply gone, replaced in place.
func reExecSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding own executable path: %w", err)
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
