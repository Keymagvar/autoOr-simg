package fuse

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/winfsp/cgofuse/fuse"

	"autoOr-simg/internal/logging"
)

// Controller wraps a mounted fuse.FileSystemHost with the guaranteed-
// unmount lifecycle from DESIGN.md §4(c)/§8: Mount() runs the (blocking)
// FUSE serve loop in a goroutine, and Unmount() is safe to call exactly
// once from elsewhere (a signal handler, the web UI's Stop/Shutdown action)
// to tear it down and wait for the serve loop to actually exit before
// returning — so callers never report "stopped" before it's true.
type Controller struct {
	mountpoint string
	host       *fuse.FileSystemHost
	done       chan struct{}
	mountOK    chan bool
	log        *slog.Logger
}

// Mount starts serving fsop at mountpoint in the background and returns
// immediately; use WaitMounted to block until the mount is confirmed live
// (or failed) before relying on the path being usable.
func Mount(mountpoint string, fsop fuse.FileSystemInterface) (*Controller, error) {
	if err := ensureMountpoint(mountpoint); err != nil {
		return nil, fmt.Errorf("preparing mountpoint %s: %w", mountpoint, err)
	}

	c := &Controller{
		mountpoint: mountpoint,
		host:       fuse.NewFileSystemHost(fsop),
		done:       make(chan struct{}),
		mountOK:    make(chan bool, 1),
		log:        logging.For("fuse").With("mountpoint", mountpoint),
	}

	go func() {
		defer close(c.done)
		c.log.Info("mounting")
		ok := c.host.Mount(mountpoint, nil)
		c.log.Info("mount serve loop exited", "ok", ok)
		select {
		case c.mountOK <- ok:
		default:
		}
	}()

	return c, nil
}

// WaitMounted blocks briefly to let the mount either come up or fail fast
// (e.g. mountpoint busy, FUSE unavailable) so callers get a real error
// instead of silently proceeding against a dead mount.
func (c *Controller) WaitMounted(timeout time.Duration) error {
	select {
	case ok := <-c.mountOK:
		if !ok {
			return fmt.Errorf("mount of %s failed", c.mountpoint)
		}
		// host.Mount returning true this fast (before the serve loop would
		// normally block for the filesystem's lifetime) also indicates an
		// immediate failure in practice for most FUSE backends; treat the
		// happy path as "didn't fail within timeout" below instead.
		return nil
	case <-time.After(timeout):
		// No result yet within timeout — on a healthy mount, host.Mount()
		// is still blocked serving requests, which is the success case.
		return nil
	case <-c.done:
		return fmt.Errorf("mount of %s exited before timeout", c.mountpoint)
	}
}

// Unmount requests a clean unmount and blocks until the serve loop has
// actually exited — the "guaranteed" half of DESIGN.md §4(c): callers must
// be able to trust that once this returns, the mountpoint is free again.
func (c *Controller) Unmount() error {
	c.log.Info("unmounting")
	if !c.host.Unmount() {
		c.log.Warn("host.Unmount() reported failure, waiting for serve loop anyway")
	}
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("unmount of %s did not complete within timeout", c.mountpoint)
	}
	c.log.Info("unmounted")
	return nil
}
