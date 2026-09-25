//go:build windows

package bypass

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

const ipUnicastIF = 31

func Protect4(fd uintptr, ifIndex, mark int) {
	if ifIndex <= 0 {
		return
	}
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(ifIndex))
	v := *(*int32)(unsafe.Pointer(&b[0]))
	_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, ipUnicastIF, int(v))
	_ = mark
}
