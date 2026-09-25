package rfbsrv

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

type fakeCap struct{ f Frame }

func (f fakeCap) Grab() (Frame, error)      { return f.f, nil }
func (fakeCap) Pointer(int, int, int) error { return nil }
func (fakeCap) Key(bool, uint32) error      { return nil }

func TestSolidTile(t *testing.T) {
	pix := make([]byte, 16*16*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i+2] = 255
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan error, 1)
	go func() { done <- Serve(a, fakeCap{f: Frame{W: 16, H: 16, Pix: pix}}) }()

	_ = b.SetDeadline(time.Now().Add(5 * time.Second))
	ver := make([]byte, 12)
	if _, err := io.ReadFull(b, ver); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	sec := make([]byte, 2)
	if _, err := io.ReadFull(b, sec); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	res := make([]byte, 4)
	if _, err := io.ReadFull(b, res); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	init := make([]byte, 24)
	if _, err := io.ReadFull(b, init); err != nil {
		t.Fatal(err)
	}
	nameLen := binary.BigEndian.Uint32(init[20:24])
	if _, err := io.CopyN(io.Discard, b, int64(nameLen)); err != nil {
		t.Fatal(err)
	}
	req := make([]byte, 10)
	req[0] = 3
	binary.BigEndian.PutUint16(req[6:8], 16)
	binary.BigEndian.PutUint16(req[8:10], 16)
	if _, err := b.Write(req); err != nil {
		t.Fatal(err)
	}
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(b, hdr); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(hdr[2:4]) != 1 {
		t.Fatalf("rects %d", binary.BigEndian.Uint16(hdr[2:4]))
	}
	rh := make([]byte, 12)
	if _, err := io.ReadFull(b, rh); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(rh[8:12]) != 5 {
		t.Fatalf("encoding %d", binary.BigEndian.Uint32(rh[8:12]))
	}
	body := make([]byte, 5)
	if _, err := io.ReadFull(b, body); err != nil {
		t.Fatal(err)
	}
	if body[0] != 2 || body[3] != 255 {
		t.Fatalf("hextile %v", body)
	}
	b.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit")
	}
}
