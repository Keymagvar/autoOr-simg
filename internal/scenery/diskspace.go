package scenery

// FreeBytes reports free space available on the filesystem containing path.
// ok is false when the platform-specific implementation couldn't determine
// it (DESIGN.md §11 only requires a preflight *when we can check* — skipping
// gracefully on an unsupported platform is safer than guessing).
func FreeBytes(path string) (free uint64, ok bool, err error) {
	return freeBytes(path)
}
