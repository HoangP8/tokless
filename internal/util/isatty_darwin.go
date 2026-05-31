//go:build darwin

package util

import (
	"syscall"
	"unsafe"
)

const ioctlReadTermios = 0x40487413 // TIOCGETA

// isTerminal reports whether fd is a real terminal (not a pipe or /dev/null).
func isTerminal(fd uintptr) bool {
	var t syscall.Termios
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, ioctlReadTermios,
		uintptr(unsafe.Pointer(&t)), 0, 0, 0)
	return err == 0
}
