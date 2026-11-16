#!/usr/bin/env bash
# Continuously snapshots the autoortho pprof heap endpoint until stopped (Ctrl-C).
# Usage: ./pprof_loop.sh [output_dir] [interval_seconds]
set -euo pipefail

OUT="${1:-./pprof_snapshots}"
INTERVAL="${2:-5}"
URL="http://127.0.0.1:6061/debug/pprof/heap"

mkdir -p "$OUT"
echo "Snapshotting $URL to $OUT every ${INTERVAL}s. Ctrl-C to stop."

while true; do
  ts="$(date +%Y%m%d_%H%M%S)"
  if curl -s -m 5 "$URL" -o "$OUT/heap_$ts.pprof"; then
    rss="$(ps -o rss= -p "$(pgrep -f '[.]/autoortho run' | head -1)" 2>/dev/null || echo '?')"
    echo "$ts  rss_kb=$rss  -> heap_$ts.pprof"
  else
    echo "$ts  pprof endpoint unreachable (process likely dead)"
  fi
  sleep "$INTERVAL"
done
