//go:build windows

package engine

// setLingerZero is a no-op on Windows.
// Windows does not support SetsockoptLinger with int fd; it requires syscall.Handle.
// Skipping SO_LINGER is acceptable for a load testing tool running on Windows.
func setLingerZero(fd uintptr) error {
	return nil
}
