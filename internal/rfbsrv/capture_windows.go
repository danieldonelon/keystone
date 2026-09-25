//go:build windows

package rfbsrv

import (
	"encoding/binary"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	gdi32                = windows.NewLazySystemDLL("gdi32.dll")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procSetDPIAware      = user32.NewProc("SetProcessDPIAware")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procSendInput        = user32.NewProc("SendInput")
	procVkKeyScan        = user32.NewProc("VkKeyScanW")
	procCreateCompatDC   = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatBmp  = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procBitBlt           = gdi32.NewProc("BitBlt")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procDeleteDC         = gdi32.NewProc("DeleteDC")
	procGetDIBits        = gdi32.NewProc("GetDIBits")
)

const (
	smXVirtual  = 76
	smYVirtual  = 77
	smCXVirtual = 78
	smCYVirtual = 79
	srcCopy     = 0x00CC0020
	mouseMove   = 0x0001
	mouseAbs    = 0x8000
	mouseVDesk  = 0x4000
	mouseLeftD  = 0x0002
	mouseLeftU  = 0x0004
	mouseRightD = 0x0008
	mouseRightU = 0x0010
	mouseMidD   = 0x0020
	mouseMidU   = 0x0040
	mouseWheel  = 0x0800
	keyUpFlag   = 0x0002
	keyUnicode  = 0x0004
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type Desktop struct {
	mu      sync.Mutex
	buttons int
	vx, vy  int
	vw, vh  int
	aware   bool
}

func NewDesktop() *Desktop { return &Desktop{} }

func (d *Desktop) Grab() (Frame, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.aware {
		procSetDPIAware.Call()
		d.aware = true
	}
	vx, _, _ := procGetSystemMetrics.Call(smXVirtual)
	vy, _, _ := procGetSystemMetrics.Call(smYVirtual)
	vw, _, _ := procGetSystemMetrics.Call(smCXVirtual)
	vh, _, _ := procGetSystemMetrics.Call(smCYVirtual)
	w, h := int(vw), int(vh)
	if w <= 0 || h <= 0 {
		w, h = 1, 1
	}
	d.vx, d.vy, d.vw, d.vh = int(vx), int(vy), w, h
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return Frame{}, syscall.EINVAL
	}
	defer procReleaseDC.Call(0, hdc)
	mem, _, _ := procCreateCompatDC.Call(hdc)
	if mem == 0 {
		return Frame{}, syscall.EINVAL
	}
	defer procDeleteDC.Call(mem)
	bmp, _, _ := procCreateCompatBmp.Call(hdc, uintptr(w), uintptr(h))
	if bmp == 0 {
		return Frame{}, syscall.EINVAL
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(mem, bmp)
	defer procSelectObject.Call(mem, old)
	procBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), hdc, vx, vy, srcCopy)
	pix := make([]byte, w*h*4)
	var bi bitmapInfoHeader
	bi.Size = 40
	bi.Width = int32(w)
	bi.Height = -int32(h)
	bi.Planes = 1
	bi.BitCount = 32
	procGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&bi)), 0)
	return Frame{W: w, H: h, Pix: pix}, nil
}

func (d *Desktop) Pointer(x, y, buttons int) error {
	d.mu.Lock()
	prev := d.buttons
	d.buttons = buttons
	w, h := d.vw, d.vh
	d.mu.Unlock()
	if w <= 1 {
		w = 2
	}
	if h <= 1 {
		h = 2
	}
	flags := uint32(mouseMove | mouseAbs | mouseVDesk)
	flags |= edge(prev, buttons, 1, mouseLeftD, mouseLeftU)
	flags |= edge(prev, buttons, 2, mouseMidD, mouseMidU)
	flags |= edge(prev, buttons, 4, mouseRightD, mouseRightU)
	data := uint32(0)
	if buttons&8 != 0 && prev&8 == 0 {
		flags |= mouseWheel
		data = 120
	}
	if buttons&16 != 0 && prev&16 == 0 {
		flags |= mouseWheel
		data = 0xFFFFFFFF - 119
	}
	dx := int32(x * 65535 / (w - 1))
	dy := int32(y * 65535 / (h - 1))
	return sendMouse(dx, dy, flags, data)
}

func edge(prev, cur, bit int, down, up uint32) uint32 {
	was := prev&bit != 0
	now := cur&bit != 0
	if now && !was {
		return down
	}
	if was && !now {
		return up
	}
	return 0
}

func (d *Desktop) Key(down bool, keysym uint32) error {
	if vk, ok := keysymVK(keysym); ok {
		flags := uint32(0)
		if !down {
			flags = keyUpFlag
		}
		return sendKey(vk, 0, flags)
	}
	if keysym >= 32 && keysym < 0x10000 {
		flags := uint32(keyUnicode)
		if !down {
			flags |= keyUpFlag
		}
		return sendKey(0, uint16(keysym), flags)
	}
	return nil
}

func keysymVK(k uint32) (uint16, bool) {
	table := map[uint32]uint16{
		0xff08: 0x08, 0xff09: 0x09, 0xff0d: 0x0D, 0xff1b: 0x1B,
		0xff50: 0x24, 0xff51: 0x25, 0xff52: 0x26, 0xff53: 0x27, 0xff54: 0x28,
		0xff55: 0x21, 0xff56: 0x22, 0xff57: 0x23, 0xffff: 0x2E, 0xff63: 0x2D,
		0xffe1: 0x10, 0xffe2: 0x10, 0xffe3: 0x11, 0xffe4: 0x11,
		0xffe9: 0x12, 0xffea: 0x12, 0xffeb: 0x5B, 0xffec: 0x5C,
	}
	if v, ok := table[k]; ok {
		return v, true
	}
	if k >= 0xffbe && k <= 0xffc9 {
		return uint16(0x70 + (k - 0xffbe)), true
	}
	if k >= 0x20 && k <= 0x7e {
		r, _, _ := procVkKeyScan.Call(uintptr(k))
		if int16(r) != -1 {
			return uint16(r & 0xff), true
		}
	}
	return 0, false
}

func sendMouse(dx, dy int32, flags, data uint32) error {
	var buf [40]byte
	binary.LittleEndian.PutUint32(buf[8:12], uint32(dx))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(dy))
	binary.LittleEndian.PutUint32(buf[16:20], data)
	binary.LittleEndian.PutUint32(buf[20:24], flags)
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&buf[0])), 40)
	if r != 1 {
		return err
	}
	return nil
}

func sendKey(vk, scan uint16, flags uint32) error {
	var buf [40]byte
	binary.LittleEndian.PutUint32(buf[0:4], 1)
	binary.LittleEndian.PutUint16(buf[8:10], vk)
	binary.LittleEndian.PutUint16(buf[10:12], scan)
	binary.LittleEndian.PutUint32(buf[12:16], flags)
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&buf[0])), 40)
	if r != 1 {
		return err
	}
	return nil
}

func ProbeInput() error {
	return sendMouse(0, 0, mouseMove, 0)
}
