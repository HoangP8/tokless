//go:build !windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

func ttyWidth() int {
	ws := &winsize{}
	t := os.Stdout.Fd()
	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, t, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(ws))); err != 0 {
		return -1
	}
	if ws.col == 0 {
		return -1
	}
	return int(ws.col)
}

type winsize struct {
	row, col, xpixel, ypixel uint16
}
