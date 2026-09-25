//go:build linux

package bypass

import (
	"net"

	"golang.org/x/sys/unix"
)

func Protect4(fd uintptr, ifIndex, mark int) {
	if mark != 0 {
		_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
	}
	if ifIndex <= 0 {
		return
	}
	ifi, err := net.InterfaceByIndex(ifIndex)
	if err != nil {
		return
	}
	_ = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifi.Name)
}
