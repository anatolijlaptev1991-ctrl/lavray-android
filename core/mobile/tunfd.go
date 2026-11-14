// SPDX-License-Identifier: GPL-3.0-only

//go:build linux

package mobile

// tunfd.go — операции над дескриптором TUN (порт libbox/tun_name_linux.go).

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const ifReqSize = unix.IFNAMSIZ + 64

// tunnelName спрашивает у ядра имя интерфейса через ioctl TUNGETIFF.
func tunnelName(fd int32) (string, error) {
	var ifr [ifReqSize]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNGETIFF), uintptr(unsafe.Pointer(&ifr[0])))
	if errno != 0 {
		return "", fmt.Errorf("failed to get name of TUN device: %w", syscall.Errno(errno))
	}
	return unix.ByteSliceToString(ifr[:]), nil
}

// closeFd закрывает дескриптор, которым владеет Go.
func closeFd(fd int) { _ = syscall.Close(fd) }
