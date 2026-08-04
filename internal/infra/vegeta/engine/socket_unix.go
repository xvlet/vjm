//go:build !windows

package engine

import (
	"syscall"
)

// setLingerZero sets SO_LINGER with l_onoff=1, l_linger=0 on the socket.
// This causes RST to be sent on close instead of FIN, immediately freeing the port.
func setLingerZero(fd uintptr) error {
	return syscall.SetsockoptLinger(int(fd), syscall.SOL_SOCKET, syscall.SO_LINGER, &syscall.Linger{Onoff: 1, Linger: 0})
}
