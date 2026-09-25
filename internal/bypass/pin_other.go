//go:build !windows && !linux

package bypass

func Protect4(fd uintptr, ifIndex, mark int) {}
