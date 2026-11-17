// Package memwatch is the DESIGN.md §9 memory watchdog: a staged response
// to process memory growth that works independent of any one subsystem's
// own bookkeeping (internal/tile's Cacher only ever reasons about its own
// tile count, never "is the whole process dangerously large"). It samples
// real process RSS — not any internal counter — against thresholds
// expressed as a fraction of total system memory, so one sensible default
// applies whether this runs on an 8GB laptop or a 64GB desktop.
//
// Motivated by a real, reproduced finding, not a theoretical gap: the
// first live test flight drove RSS to ~20GB
// within about ten minutes of normal X-Plane scenery loading at one
// airport, because internal/tile's Cacher can only evict tiles with no
// outstanding refs, and X-Plane was legitimately holding many tiles open
// at once. This package is the safety net for exactly that case — it
// cannot, by itself, stop RSS from growing when every cached tile is
// genuinely in use (see Cacher.ForceEvictIdle's doc comment), so Critical
// exists specifically to turn "uncontrolled OOM, possibly taking down
// something else on the machine" into "this process shuts down cleanly on
// its own terms" once Soft-stage eviction isn't enough.
package memwatch

import (
	"context"
	"log/slog"
	"time"

	"autoOr-simg/internal/logging"
)

// Thresholds are absolute byte values, usually derived from a percentage
// of total system memory via NewThresholdsFromPercent — kept as plain
// bytes on the struct itself so Watchdog's hot path is just three
// comparisons, no repeated percentage math.
type Thresholds struct {
	Soft     uint64
	Hard     uint64
	Critical uint64
}

// NewThresholdsFromPercent turns three fractions of totalBytes (e.g. 0.50,
// 0.70, 0.85) into absolute Thresholds. Mirrors config.Memory's
// SoftPercent/HardPercent/CriticalPercent fields.
func NewThresholdsFromPercent(totalBytes uint64, soft, hard, critical float64) Thresholds {
	return Thresholds{
		Soft:     uint64(float64(totalBytes) * soft),
		Hard:     uint64(float64(totalBytes) * hard),
		Critical: uint64(float64(totalBytes) * critical),
	}
}

// Watchdog periodically samples process RSS and calls the matching
// callback for the highest threshold currently crossed. Soft fires every
// tick it's in effect (eviction is meant to be a repeated, cheap action,
// not a one-shot); Hard logs only on each new entry into the Hard zone,
// not every tick, to avoid flooding logs during a sustained high-memory
// period; Critical fires at most once (Watchdog stops ticking after,
// since the expected caller action is an irreversible shutdown).
type Watchdog struct {
	thresholds Thresholds
	interval   time.Duration

	// readRSS is overridable for tests — production use leaves it nil and
	// gets the real /proc/self/status reader.
	readRSS func() (uint64, error)

	onSoft     func()
	onHard     func(rssBytes, totalBytes uint64)
	onCritical func(rssBytes, totalBytes uint64)

	totalBytes uint64
	log        *slog.Logger
}

// New builds a Watchdog sampling real process RSS every interval against
// thresholds. onSoft/onHard/onCritical may be nil (that stage is then
// silently skipped) but Critical should always be set in production — it
// is the entire point of this package.
func New(thresholds Thresholds, totalBytes uint64, interval time.Duration, onSoft func(), onHard, onCritical func(rssBytes, totalBytes uint64)) *Watchdog {
	return &Watchdog{
		thresholds: thresholds,
		interval:   interval,
		readRSS:    ReadRSSBytes,
		onSoft:     onSoft,
		onHard:     onHard,
		onCritical: onCritical,
		totalBytes: totalBytes,
		log:        logging.For("memwatch"),
	}
}

// Run blocks, sampling every interval until ctx is cancelled or Critical
// fires (whichever first). Intended to be started in its own goroutine.
func (w *Watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	wasHard := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rss, err := w.readRSS()
			if err != nil {
				w.log.Warn("reading process RSS failed, skipping this tick", "err", err)
				continue
			}

			if rss >= w.thresholds.Critical {
				w.log.Error("CRITICAL memory threshold crossed — triggering safe shutdown",
					"rss_bytes", rss, "total_bytes", w.totalBytes, "critical_threshold_bytes", w.thresholds.Critical)
				if w.onCritical != nil {
					w.onCritical(rss, w.totalBytes)
				}
				return // stop watching — the expected action is an irreversible shutdown
			}

			inHard := rss >= w.thresholds.Hard
			if inHard && !wasHard {
				w.log.Error("HARD memory threshold crossed — memory usage is high",
					"rss_bytes", rss, "total_bytes", w.totalBytes, "hard_threshold_bytes", w.thresholds.Hard)
			}
			wasHard = inHard
			if inHard && w.onHard != nil {
				w.onHard(rss, w.totalBytes)
			}

			// Soft-stage eviction runs any time RSS is at or above Soft,
			// including while also above Hard — it's a cheap, repeated,
			// always-safe action, not mutually exclusive with Hard's
			// warning.
			if rss >= w.thresholds.Soft && w.onSoft != nil {
				w.onSoft()
			}
		}
	}
}

// ReadRSSBytes reads this process's current resident set size, and
// TotalSystemMemoryBytes reads total system RAM — both real platform
// queries, not an internal counter. Implemented per-platform:
// watchdog_linux.go (/proc), watchdog_windows.go (GetProcessMemoryInfo/
// GlobalMemoryStatusEx), watchdog_darwin.go (getrusage/sysctl hw.memsize).
// Until this session, only the Linux path existed at all, unconditionally
// (no build tag): it compiled fine everywhere — plain Go, just a
// hardcoded "/proc/..." path — but failed at runtime on Windows/macOS,
// where that path simply doesn't exist. This package's entire safety net
// silently never worked anywhere but Linux, which nothing short of an
// actual run on those platforms could have caught (confirmed the hard
// way, by a real windows-latest CI run).
