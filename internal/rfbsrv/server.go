package rfbsrv

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"
)

type Frame struct {
	W, H int
	Pix  []byte // BGRA, top-down, stride W*4
}

type Capturer interface {
	Grab() (Frame, error)
	Pointer(x, y, buttons int) error
	Key(down bool, keysym uint32) error
}

func Serve(conn net.Conn, cap Capturer) error {
	defer conn.Close()
	if _, err := io.WriteString(conn, "RFB 003.008\n"); err != nil {
		return err
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(conn, ver); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{1, 1}); err != nil {
		return err
	}
	choice := make([]byte, 1)
	if _, err := io.ReadFull(conn, choice); err != nil {
		return err
	}
	if choice[0] != 1 {
		return errors.New("viewer chose unsupported security")
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return err
	}
	shared := make([]byte, 1)
	if _, err := io.ReadFull(conn, shared); err != nil {
		return err
	}
	frame, err := cap.Grab()
	if err != nil {
		return err
	}
	if err := writeServerInit(conn, frame.W, frame.H); err != nil {
		return err
	}
	events := make(chan any, 32)
	errc := make(chan error, 1)
	go func() {
		errc <- readClient(conn, events)
	}()
	var last Frame
	want := false
	incremental := true
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			switch e := ev.(type) {
			case fbu:
				want = true
				incremental = e.inc
			case pointerEv:
				_ = cap.Pointer(e.x, e.y, e.buttons)
			case keyEv:
				_ = cap.Key(e.down, e.keysym)
			}
		case err := <-errc:
			return err
		case <-tick.C:
			if !want {
				continue
			}
			frame, err := cap.Grab()
			if err != nil {
				return err
			}
			if err := writeUpdate(conn, frame, last, incremental); err != nil {
				return err
			}
			last = frame
			want = false
		}
	}
}

func writeServerInit(conn net.Conn, w, h int) error {
	buf := make([]byte, 24+8)
	binary.BigEndian.PutUint16(buf[0:2], uint16(w))
	binary.BigEndian.PutUint16(buf[2:4], uint16(h))
	buf[4] = 32
	buf[5] = 24
	buf[6] = 0
	buf[7] = 1
	binary.BigEndian.PutUint16(buf[8:10], 255)
	binary.BigEndian.PutUint16(buf[10:12], 255)
	binary.BigEndian.PutUint16(buf[12:14], 255)
	buf[14] = 16
	buf[15] = 8
	buf[16] = 0
	name := []byte("Keystone")
	binary.BigEndian.PutUint32(buf[20:24], uint32(len(name)))
	buf = append(buf[:24], name...)
	_, err := conn.Write(buf)
	return err
}

type fbu struct{ inc bool }
type pointerEv struct{ x, y, buttons int }
type keyEv struct {
	down   bool
	keysym uint32
}

func readClient(conn net.Conn, events chan any) error {
	defer close(events)
	for {
		var t [1]byte
		if _, err := io.ReadFull(conn, t[:]); err != nil {
			return err
		}
		switch t[0] {
		case 0:
			rest := make([]byte, 19)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
		case 2:
			hdr := make([]byte, 3)
			if _, err := io.ReadFull(conn, hdr); err != nil {
				return err
			}
			n := int(binary.BigEndian.Uint16(hdr[1:3]))
			if _, err := io.CopyN(io.Discard, conn, int64(n)*4); err != nil {
				return err
			}
		case 3:
			rest := make([]byte, 9)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
			events <- fbu{inc: rest[0] != 0}
		case 4:
			rest := make([]byte, 7)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
			events <- keyEv{down: rest[0] != 0, keysym: binary.BigEndian.Uint32(rest[3:7])}
		case 5:
			rest := make([]byte, 5)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
			events <- pointerEv{
				buttons: int(rest[0]),
				x:       int(binary.BigEndian.Uint16(rest[1:3])),
				y:       int(binary.BigEndian.Uint16(rest[3:5])),
			}
		case 6:
			rest := make([]byte, 7)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
			n := binary.BigEndian.Uint32(rest[3:7])
			if _, err := io.CopyN(io.Discard, conn, int64(n)); err != nil {
				return err
			}
		default:
			return errors.New("unknown client message")
		}
	}
}

func writeUpdate(conn net.Conn, frame, prev Frame, incremental bool) error {
	if frame.W == 0 || frame.H == 0 || len(frame.Pix) < frame.W*frame.H*4 {
		return errors.New("bad frame")
	}
	type rect struct {
		x, y, w, h int
		raw        bool
		pix        []byte
	}
	var rects []rect
	sameSize := prev.W == frame.W && prev.H == frame.H && len(prev.Pix) == len(frame.Pix)
	for y := 0; y < frame.H; y += 16 {
		for x := 0; x < frame.W; x += 16 {
			tw := min(16, frame.W-x)
			th := min(16, frame.H-y)
			tile := crop(frame, x, y, tw, th)
			if incremental && sameSize && sameTile(frame, prev, x, y, tw, th) {
				continue
			}
			if solid(tile) {
				rects = append(rects, rect{x: x, y: y, w: tw, h: th, pix: append([]byte(nil), tile[:4]...)})
			} else {
				rects = append(rects, rect{x: x, y: y, w: tw, h: th, raw: true, pix: tile})
			}
		}
	}
	if len(rects) > 65535 {
		rects = rects[:65535]
	}
	hdr := make([]byte, 4)
	hdr[0] = 0
	binary.BigEndian.PutUint16(hdr[2:4], uint16(len(rects)))
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	for _, rc := range rects {
		rh := make([]byte, 12)
		binary.BigEndian.PutUint16(rh[0:2], uint16(rc.x))
		binary.BigEndian.PutUint16(rh[2:4], uint16(rc.y))
		binary.BigEndian.PutUint16(rh[4:6], uint16(rc.w))
		binary.BigEndian.PutUint16(rh[6:8], uint16(rc.h))
		if rc.raw {
			binary.BigEndian.PutUint32(rh[8:12], 0)
			if _, err := conn.Write(rh); err != nil {
				return err
			}
			if _, err := conn.Write(rc.pix); err != nil {
				return err
			}
			continue
		}
		binary.BigEndian.PutUint32(rh[8:12], 5)
		if _, err := conn.Write(rh); err != nil {
			return err
		}
		body := []byte{2, rc.pix[0], rc.pix[1], rc.pix[2], rc.pix[3]}
		if _, err := conn.Write(body); err != nil {
			return err
		}
	}
	return nil
}

func crop(f Frame, x, y, w, h int) []byte {
	out := make([]byte, w*h*4)
	stride := f.W * 4
	for row := 0; row < h; row++ {
		src := (y+row)*stride + x*4
		copy(out[row*w*4:(row+1)*w*4], f.Pix[src:src+w*4])
	}
	return out
}

func sameTile(a, b Frame, x, y, w, h int) bool {
	stride := a.W * 4
	for row := 0; row < h; row++ {
		off := (y+row)*stride + x*4
		if string(a.Pix[off:off+w*4]) != string(b.Pix[off:off+w*4]) {
			return false
		}
	}
	return true
}

func solid(pix []byte) bool {
	if len(pix) < 4 {
		return false
	}
	for i := 4; i < len(pix); i += 4 {
		if pix[i] != pix[0] || pix[i+1] != pix[1] || pix[i+2] != pix[2] || pix[i+3] != pix[3] {
			return false
		}
	}
	return true
}
