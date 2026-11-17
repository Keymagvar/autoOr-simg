//go:build windows

package memwatch

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processMemoryCounters mirrors Windows' PROCESS_MEMORY_COUNTERS
// (psapi.h) field-for-field, including the fields this package never
// reads — GetProcessMemoryInfo writes into this buffer based on byte
// offsets, keyed off the cb size we pass it, so the Go struct's layout
// and size have to match the real C one exactly, not just the one field
// actually used below.
type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

// memoryStatusEx mirrors Windows' MEMORYSTATUSEX (sysinfoapi.h). dwLength
// must be set to this struct's size before calling GlobalMemoryStatusEx —
// a documented, easy-to-forget requirement of that API.
type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

var (
	modpsapi                 = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")

	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

// ReadRSSBytes reads this process's current working set size — Windows'
// closest analog to Linux RSS (and what every other tool, e.g. psutil,
// reports as "rss" on Windows) — via psapi's GetProcessMemoryInfo.
func ReadRSSBytes() (uint64, error) {
	var counters processMemoryCounters
	counters.cb = uint32(unsafe.Sizeof(counters))

	r, _, err := procGetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.cb),
	)
	if r == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo: %w", err)
	}
	return uint64(counters.workingSetSize), nil
}

// TotalSystemMemoryBytes reads total physical RAM via kernel32's
// GlobalMemoryStatusEx — used once at startup to turn config.Memory's
// percentages into absolute Thresholds.
func TotalSystemMemoryBytes() (uint64, error) {
	var status memoryStatusEx
	status.dwLength = uint32(unsafe.Sizeof(status))

	r, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if r == 0 {
		return 0, fmt.Errorf("GlobalMemoryStatusEx: %w", err)
	}
	return status.ullTotalPhys, nil
}
