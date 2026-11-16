//go:build cgo && linux

package native

/*
#include <malloc.h>
*/
import "C"

// init caps glibc's malloc arena count via mallopt(M_ARENA_MAX, ...) before
// any heavy concurrent cgo allocation happens (TurboJPEG decode, ISPC BC1
// compress — both go straight to C malloc/free, invisible to Go's GC and
// its heap profiler).
//
// This is the real fix for the second half of the Berlin leak:
// after fixing the first (abandoned chunk fetches still decoding — see
// tile.go), a real flight's RSS still climbed well past what pprof's own
// heap profile could account for (an ~8.7GB gap between RSS and Go-tracked
// heap after ~7 minutes). Reproduced safely without X-Plane via
// tools/fuse_read_stress.py (reading a continuously-expanding set of
// never-before-seen tiles through the real FUSE path — a fixed/reused tile
// set never triggered it) and confirmed to be glibc's default
// multi-arena behavior: under many goroutines concurrently calling into
// cgo as fresh tiles are decoded/compressed, glibc spins up additional
// malloc arenas that don't shrink back, inflating RSS with memory Go's
// allocator never sees. `MALLOC_ARENA_MAX=2` eliminated the growth
// entirely in that same repro (RSS pinned flat instead of ratcheting from
// ~4.9GB to ~8.4GB over 5 minutes) — mallopt is the same knob, set from
// inside the binary instead of relying on an external env var at launch.
//
// 2, not 1: a single arena serializes every malloc/free across every OS
// thread process-wide, which would needlessly throttle concurrent cgo
// calls under this process's real concurrency (fetch_threads=32 by
// default); 2 bounds the worst-case bloat Soft/Hard memwatch can respond
// to without reintroducing meaningful single-arena contention.
func init() {
	C.mallopt(C.M_ARENA_MAX, 2)
}
