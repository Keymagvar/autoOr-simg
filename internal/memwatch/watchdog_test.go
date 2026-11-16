package memwatch

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadRSSBytes_RealProcess(t *testing.T) {
	rss, err := ReadRSSBytes()
	if err != nil {
		t.Fatalf("ReadRSSBytes: %v", err)
	}
	if rss == 0 {
		t.Error("a running test process should report nonzero RSS")
	}
	// Sanity bound: a go test binary is not plausibly using more than
	// 32GB RSS just existing — catches a parsing bug that scaled by the
	// wrong unit (e.g. not converting kB to bytes) rather than genuinely
	// validating a specific number.
	if rss > 32*1024*1024*1024 {
		t.Errorf("ReadRSSBytes() = %d bytes, implausibly large for a test process — likely a unit-conversion bug", rss)
	}
}

func TestTotalSystemMemoryBytes_RealSystem(t *testing.T) {
	total, err := TotalSystemMemoryBytes()
	if err != nil {
		t.Fatalf("TotalSystemMemoryBytes: %v", err)
	}
	if total == 0 {
		t.Error("a real system should report nonzero total memory")
	}
	rss, err := ReadRSSBytes()
	if err != nil {
		t.Fatalf("ReadRSSBytes: %v", err)
	}
	if rss > total {
		t.Errorf("this process's RSS (%d) exceeds total system memory (%d) — one of the two readers is wrong", rss, total)
	}
}

func TestNewThresholdsFromPercent(t *testing.T) {
	th := NewThresholdsFromPercent(1000, 0.5, 0.7, 0.85)
	if th.Soft != 500 || th.Hard != 700 || th.Critical != 850 {
		t.Errorf("got %+v, want {500 700 850}", th)
	}
}

// TestWatchdog_StagedResponse drives the watchdog through a scripted RSS
// sequence (below soft -> soft -> hard -> critical) via an injected fake
// reader, and verifies each callback fires exactly where expected — the
// real logic under test, independent of actual memory pressure.
func TestWatchdog_StagedResponse(t *testing.T) {
	sequence := []uint64{10, 60, 85, 96} // out of total=100, thresholds 50/80/95
	var idx atomic.Int32

	w := New(
		Thresholds{Soft: 50, Hard: 80, Critical: 95},
		100,
		5*time.Millisecond,
		nil, nil, nil, // replaced below
	)
	w.readRSS = func() (uint64, error) {
		i := idx.Load()
		if int(i) >= len(sequence) {
			return sequence[len(sequence)-1], nil
		}
		idx.Add(1)
		return sequence[i], nil
	}

	var mu sync.Mutex
	var softCalls, hardCalls int
	var criticalCalled bool
	var criticalRSS uint64

	w.onSoft = func() {
		mu.Lock()
		softCalls++
		mu.Unlock()
	}
	w.onHard = func(rss, total uint64) {
		mu.Lock()
		hardCalls++
		mu.Unlock()
	}
	w.onCritical = func(rss, total uint64) {
		mu.Lock()
		criticalCalled = true
		criticalRSS = rss
		mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w.Run(ctx) // returns on its own once Critical fires

	mu.Lock()
	defer mu.Unlock()

	if !criticalCalled {
		t.Fatal("expected onCritical to fire once the scripted RSS sequence reached the critical threshold")
	}
	if criticalRSS != 96 {
		t.Errorf("onCritical rss = %d, want 96 (the scripted value that crossed Critical)", criticalRSS)
	}
	if softCalls == 0 {
		t.Error("expected onSoft to have fired at least once while RSS was between Soft and Hard")
	}
	if hardCalls == 0 {
		t.Error("expected onHard to have fired at least once while RSS was between Hard and Critical")
	}
}

// TestWatchdog_StopsOnContextCancel confirms Run returns promptly when ctx
// is cancelled and RSS never crosses Critical — the "nothing wrong, just
// shutting down normally" path, which must not hang.
func TestWatchdog_StopsOnContextCancel(t *testing.T) {
	w := New(Thresholds{Soft: 50, Hard: 80, Critical: 95}, 100, 5*time.Millisecond, nil, nil, nil)
	w.readRSS = func() (uint64, error) { return 1, nil } // always well under every threshold

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond) // let a few ticks happen
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of context cancellation")
	}
}
