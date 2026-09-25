package bypass

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"time"
)

func MappedAddr(ctx context.Context, ifIndex, mark int) (string, error) {
	var d net.ListenConfig
	d.Control = func(network, address string, c syscall.RawConn) error {
		return c.Control(func(fd uintptr) { Protect4(fd, ifIndex, mark) })
	}
	pc, err := d.ListenPacket(ctx, "udp4", ":0")
	if err != nil {
		return "", err
	}
	defer pc.Close()
	conn := pc.(*net.UDPConn)
	raddr, err := net.ResolveUDPAddr("udp4", "stun.l.google.com:19302")
	if err != nil {
		return "", err
	}
	req := make([]byte, 20)
	req[1] = 1
	binary.BigEndian.PutUint32(req[4:8], 0x2112A442)
	for i := 8; i < 20; i++ {
		req[i] = byte(i)
	}
	deadline := time.Now().Add(4 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	if _, err := conn.WriteToUDP(req, raddr); err != nil {
		return "", err
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return parseSTUN(buf[:n])
}

func parseSTUN(buf []byte) (string, error) {
	if len(buf) < 20 {
		return "", fmt.Errorf("short STUN reply")
	}
	off := 20
	for off+4 <= len(buf) {
		typ := binary.BigEndian.Uint16(buf[off : off+2])
		ln := int(binary.BigEndian.Uint16(buf[off+2 : off+4]))
		val := off + 4
		next := val + ln
		if next > len(buf) {
			break
		}
		if (typ == 0x0020 || typ == 0x0001) && ln >= 8 && buf[val+1] == 1 {
			var ip [4]byte
			copy(ip[:], buf[val+4:val+8])
			if typ == 0x0020 {
				magic := []byte{0x21, 0x12, 0xA4, 0x42}
				for i := 0; i < 4; i++ {
					ip[i] ^= magic[i]
				}
			}
			return net.IP(ip[:]).String(), nil
		}
		off = next
		if pad := ln % 4; pad != 0 {
			off += 4 - pad
		}
	}
	return "", fmt.Errorf("STUN reply had no mapped address")
}
