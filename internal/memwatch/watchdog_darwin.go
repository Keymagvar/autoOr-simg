//go:build darwin

package memwatch

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// ReadRSSBytes reads this process's current resident set size via
// getrusage(RUSAGE_SELF). Darwin's ru_maxrss is already reported in
// bytes — unlike Linux, where the equivalent /proc field is in kB; see
// watchdog_linux.go's own kb*1024 conversion, deliberately absent here.
func ReadRSSBytes() (uint64, error) {
	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &ru); err != nil {
		return 0, fmt.Errorf("getrusage: %w", err)
	}
	return uint64(ru.Maxrss), nil
}

// TotalSystemMemoryBytes reads total physical RAM via the hw.memsize
// sysctl — used once at startup to turn config.Memory's percentages into
// absolute Thresholds.
func TotalSystemMemoryBytes() (uint64, error) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0, fmt.Errorf("sysctl hw.memsize: %w", err)
	}
	return total, nil
}
