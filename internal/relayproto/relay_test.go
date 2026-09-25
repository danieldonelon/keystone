package relayproto

import (
	"bytes"
	"testing"
	"time"
)

func TestRelayRoundTrip(t *testing.T) {
	var src, dst [32]byte
	src[0] = 1
	dst[1] = 2
	secret := []byte("network-secret")
	payload := []byte{1, 2, 3, 4, 9}
	pkt := SealRelay(secret, src, dst, payload)
	gs, gd, gp, ok := OpenRelay(secret, pkt)
	if !ok || gs != src || gd != dst || !bytes.Equal(gp, payload) {
		t.Fatalf("open failed ok=%v", ok)
	}
	pkt[80] ^= 0xff
	if _, _, _, ok := OpenRelay(secret, pkt); ok {
		t.Fatal("tampered packet was accepted")
	}
}

func TestDiscRoundTrip(t *testing.T) {
	var pub [32]byte
	pub[3] = 7
	pkt := SealDisc([]byte("s"), pub, 51830)
	g, port, ok := OpenDisc([]byte("s"), pkt, time.Now())
	if !ok || g != pub || port != 51830 {
		t.Fatalf("disc ok=%v port=%d", ok, port)
	}
	if _, _, ok := OpenDisc([]byte("other"), pkt, time.Now()); ok {
		t.Fatal("wrong secret accepted")
	}
}
