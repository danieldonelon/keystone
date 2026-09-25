//go:build windows

package rfbsrv

import "testing"

func TestDesktopSmoke(t *testing.T) {
	frame, err := NewDesktop().Grab()
	if err != nil {
		t.Fatal(err)
	}
	if frame.W < 320 || frame.H < 240 || len(frame.Pix) != frame.W*frame.H*4 {
		t.Fatalf("frame %dx%d len %d", frame.W, frame.H, len(frame.Pix))
	}
	var nonzero int
	for _, b := range frame.Pix {
		if b != 0 {
			nonzero++
			if nonzero > 1000 {
				break
			}
		}
	}
	if nonzero < 1000 {
		t.Fatal("capture looks empty")
	}
	if err := ProbeInput(); err != nil {
		t.Fatal(err)
	}
}
