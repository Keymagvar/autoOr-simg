// Package logging sets up the structured, leveled logger used across
// autoOr-simg. See ../../DESIGN.md §6: every subsystem logs through a
// component-tagged *slog.Logger built with New(), and the on-disk log file
// is flushed on every write so it behaves like X-Plane's own Log.txt under
// `tail -f` — not a buffered file that only shows up on exit.
package logging

import (
	"io"
	"log/slog"
	"os"
	"sync"
)

// flushWriter forces a Sync/flush after every Write so the file is always
// tailable in real time, matching X-Plane's Log.txt behavior (DESIGN.md §6).
type flushWriter struct {
	mu sync.Mutex
	f  *os.File
}

func (w *flushWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.f.Write(p)
	if err == nil {
		err = w.f.Sync()
	}
	return n, err
}

// Level is the dynamic log level. It can be changed at runtime (e.g. from
// the web UI) without restarting a mount, per DESIGN.md §6.
var Level = new(slog.LevelVar)

var (
	rootOnce   sync.Once
	rootLogger *slog.Logger
	logFile    *os.File
)

// sinks are extra writers fed every log line in addition to stdout and the
// log file — used by internal/webui to backfill/stream the page's Log
// block (DESIGN.md §3) without a separate polling/tailing mechanism.
var (
	sinksMu sync.Mutex
	sinks   []io.Writer
)

// AddSink registers w to receive every log line written from here on.
// There is no corresponding RemoveSink: the one caller (the web UI's
// AppState) lives for the process lifetime.
func AddSink(w io.Writer) {
	sinksMu.Lock()
	defer sinksMu.Unlock()
	sinks = append(sinks, w)
}

type sinkBroadcastWriter struct{}

func (sinkBroadcastWriter) Write(p []byte) (int, error) {
	sinksMu.Lock()
	defer sinksMu.Unlock()
	for _, w := range sinks {
		w.Write(p)
	}
	return len(p), nil
}

// Init opens logFilePath (truncation-safe: appends) and wires up the root
// logger to write structured records to both the file and stdout. Call this
// once, early in main().
func Init(logFilePath string) (*slog.Logger, error) {
	var initErr error
	rootOnce.Do(func() {
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			initErr = err
			return
		}
		logFile = f

		fw := &flushWriter{f: f}
		writer := io.MultiWriter(os.Stdout, fw, sinkBroadcastWriter{})

		handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
			Level: Level,
		})
		rootLogger = slog.New(handler)
		slog.SetDefault(rootLogger)
	})
	if initErr != nil {
		return nil, initErr
	}
	return rootLogger, nil
}

// For returns a logger tagged with "component", mirroring the Python
// version's one logger-per-module pattern (DESIGN.md §6) so the web UI's
// log block, and anyone reading the log file, can filter by subsystem.
func For(component string) *slog.Logger {
	base := rootLogger
	if base == nil {
		base = slog.Default()
	}
	return base.With("component", component)
}

// Close flushes and closes the underlying log file. Call during shutdown
// (DESIGN.md §8) after every mount is confirmed down.
func Close() error {
	if logFile == nil {
		return nil
	}
	return logFile.Close()
}
