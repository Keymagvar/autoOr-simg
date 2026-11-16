#!/usr/bin/env python3
"""Drives the real autoortho FUSE Read() path directly, without X-Plane —
reads a *continuously expanding* window of distinct DDS tiles, mimicking a
real flight scrolling into new terrain (as opposed to revisiting a fixed
set, which the existing Cacher-level `tile stress` tool and a fixed-grid
version of this tool both failed to reproduce the real leak with).

Usage: fuse_read_stress.py <mount_dir> <base_col> <base_row> <window_n> <maptype_zoom> <duration_s> [workers] [shift_every_s]
  e.g.: fuse_read_stress.py "/path/to/z_ao_eur/textures" 24832 12416 20 BI16 300 32 5
"""
import sys
import os
import time
import threading

def main():
    mount_dir, base_col, base_row, window_n, mz, duration_s = sys.argv[1:7]
    base_col, base_row, window_n, duration_s = int(base_col), int(base_row), int(window_n), int(duration_s)
    workers = int(sys.argv[7]) if len(sys.argv) > 7 else 32
    shift_every_s = int(sys.argv[8]) if len(sys.argv) > 8 else 5

    stop_at = time.time() + duration_s
    start = time.time()
    read_count = [0]
    err_count = [0]
    lock = threading.Lock()

    def current_files():
        # Shifts the window forward over elapsed time so each shift period
        # touches tiles never seen before -- never revisits old ones,
        # matching a plane continuously flying into new terrain.
        shift = int((time.time() - start) // shift_every_s)
        bc, br = base_col + shift * window_n, base_row
        return [os.path.join(mount_dir, f"{bc+dc}_{br+dr}_{mz}.dds")
                for dc in range(window_n) for dr in range(window_n)]

    def worker(idx):
        i = idx
        while time.time() < stop_at:
            files = current_files()
            path = files[i % len(files)]
            try:
                with open(path, "rb") as f:
                    f.read(2 * 1024 * 1024)
                with lock:
                    read_count[0] += 1
            except OSError:
                with lock:
                    err_count[0] += 1
            i += workers

    threads = [threading.Thread(target=worker, args=(w,), daemon=True) for w in range(workers)]
    for t in threads:
        t.start()

    while time.time() < stop_at:
        time.sleep(5)
        with lock:
            print(f"[{int(time.time()-start)}s] reads={read_count[0]} errors={err_count[0]} window_shift={int((time.time()-start)//shift_every_s)}", flush=True)

    for t in threads:
        t.join(timeout=5)
    print(f"done. total reads={read_count[0]} errors={err_count[0]}")

if __name__ == "__main__":
    main()
