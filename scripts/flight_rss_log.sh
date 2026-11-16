#!/usr/bin/env bash
# Samples the running autoortho daemon's real process RSS every 5 seconds
# and appends a timestamped plain-text line to a log file — nothing more.
# For getting real RSS-over-time numbers from a proof flight instead of
# eyeballing a terminal.
#
# Usage: ./flight_rss_log.sh [output_file] [interval_seconds]
set -euo pipefail

OUT="${1:-./flight_rss.log}"
INTERVAL="${2:-5}"

echo "Logging RSS to $OUT every ${INTERVAL}s. Ctrl-C to stop."

while true; do
  pid="$(pgrep -f '[.]/autoortho run' | head -1)"
  ts="$(date '+%Y-%m-%d %H:%M:%S')"
  if [ -n "$pid" ]; then
    rss_kb="$(ps -o rss= -p "$pid" 2>/dev/null | tr -d ' ')"
    if [ -n "$rss_kb" ]; then
      echo "$ts  pid=$pid  rss_kb=$rss_kb" >> "$OUT"
    else
      echo "$ts  pid=$pid  rss_kb=? (ps failed)" >> "$OUT"
    fi
  else
    echo "$ts  no autoortho process found" >> "$OUT"
  fi
  sleep "$INTERVAL"
done
