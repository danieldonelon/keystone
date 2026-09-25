package wgbind

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.zx2c4.com/wireguard/conn"

	"keystone/internal/bypass"
	"keystone/internal/relayproto"
)

type Bind struct {
	mu       sync.Mutex
	conn     *net.UDPConn
	port     uint16
	ifIndex  int
	mark     int
	secret   []byte
	self     [32]byte
	haveSelf bool
	closed   bool
}

func New() *Bind { return &Bind{} }

func (b *Bind) SetIdentity(secret []byte, self [32]byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.secret = append([]byte(nil), secret...)
	b.self = self
	b.haveSelf = true
}

func (b *Bind) SetPin(ifIndex, mark int) {
	b.mu.Lock()
	b.ifIndex = ifIndex
	b.mark = mark
	c := b.conn
	b.mu.Unlock()
	if c != nil {
		applyPin(c, ifIndex, mark)
	}
}

func (b *Bind) Port() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.port)
}

func (b *Bind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		return nil, 0, conn.ErrBindAlreadyOpen
	}
	ifIndex, mark := b.ifIndex, b.mark
	var lc net.ListenConfig
	lc.Control = func(network, address string, c syscall.RawConn) error {
		return c.Control(func(fd uintptr) {
			bypass.Protect4(fd, ifIndex, mark)
		})
	}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":"+strconv.Itoa(int(port)))
	if err != nil {
		return nil, 0, err
	}
	uc := pc.(*net.UDPConn)
	ap := uc.LocalAddr().(*net.UDPAddr).AddrPort()
	b.conn = uc
	b.port = ap.Port()
	b.closed = false
	return []conn.ReceiveFunc{b.receive}, b.port, nil
}

func (b *Bind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	if b.conn != nil {
		err := b.conn.Close()
		b.conn = nil
		return err
	}
	return nil
}

func (b *Bind) SetMark(mark uint32) error {
	b.SetPin(b.currentIndex(), int(mark))
	return nil
}

func (b *Bind) currentIndex() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ifIndex
}

func (b *Bind) BatchSize() int { return 1 }

func (b *Bind) Send(bufs [][]byte, ep conn.Endpoint) error {
	b.mu.Lock()
	c := b.conn
	secret := b.secret
	self := b.self
	b.mu.Unlock()
	if c == nil {
		return net.ErrClosed
	}
	for _, buf := range bufs {
		var pkt []byte
		var dst netip.AddrPort
		switch e := ep.(type) {
		case *stdEP:
			pkt = buf
			dst = e.addr
		case *relayEP:
			if len(secret) == 0 {
				return fmt.Errorf("relay is not configured")
			}
			pkt = relayproto.SealRelay(secret, self, e.peer, buf)
			dst = e.via
		default:
			return conn.ErrWrongEndpointType
		}
		if _, err := c.WriteToUDPAddrPort(pkt, dst); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bind) ParseEndpoint(s string) (conn.Endpoint, error) {
	if strings.HasPrefix(s, "relay:") {
		rest := strings.TrimPrefix(s, "relay:")
		hexpart, addr, ok := strings.Cut(rest, "@")
		if !ok {
			return nil, fmt.Errorf("bad relay endpoint")
		}
		raw, err := hex.DecodeString(hexpart)
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("bad relay key")
		}
		ap, err := netip.ParseAddrPort(addr)
		if err != nil {
			return nil, err
		}
		var peer [32]byte
		copy(peer[:], raw)
		return &relayEP{peer: peer, via: ap}, nil
	}
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		ua, rerr := net.ResolveUDPAddr("udp4", s)
		if rerr != nil {
			return nil, err
		}
		ap = ua.AddrPort()
	}
	if !ap.Addr().Is4() {
		return nil, fmt.Errorf("keystone currently uses IPv4 endpoints")
	}
	return &stdEP{addr: ap}, nil
}

func (b *Bind) receive(bufs [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
	b.mu.Lock()
	c := b.conn
	secret := append([]byte(nil), b.secret...)
	b.mu.Unlock()
	if c == nil {
		return 0, net.ErrClosed
	}
	for {
		n, addr, err := c.ReadFromUDPAddrPort(bufs[0])
		if err != nil {
			return 0, err
		}
		if n >= 4 && string(bufs[0][:4]) == relayproto.MagicRelay {
			src, _, payload, ok := relayproto.OpenRelay(secret, bufs[0][:n])
			if !ok {
				continue
			}
			copy(bufs[0], payload)
			sizes[0] = len(payload)
			eps[0] = &relayEP{peer: src, via: addr}
			return 1, nil
		}
		if n >= 1 && bufs[0][0] == 'K' {
			continue
		}
		sizes[0] = n
		eps[0] = &stdEP{addr: addr}
		return 1, nil
	}
}

func (b *Bind) WriteControl(p []byte, to netip.AddrPort) error {
	b.mu.Lock()
	c := b.conn
	b.mu.Unlock()
	if c == nil {
		return net.ErrClosed
	}
	_, err := c.WriteToUDPAddrPort(p, to)
	return err
}

func applyPin(c *net.UDPConn, ifIndex, mark int) {
	raw, err := c.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fd uintptr) { bypass.Protect4(fd, ifIndex, mark) })
}

type stdEP struct{ addr netip.AddrPort }

func (e *stdEP) ClearSrc()           {}
func (e *stdEP) SrcToString() string { return "" }
func (e *stdEP) DstToString() string { return e.addr.String() }
func (e *stdEP) DstIP() netip.Addr   { return e.addr.Addr() }
func (e *stdEP) SrcIP() netip.Addr   { return netip.Addr{} }
func (e *stdEP) DstToBytes() []byte {
	ip := e.addr.Addr().As4()
	p := e.addr.Port()
	return []byte{ip[0], ip[1], ip[2], ip[3], byte(p >> 8), byte(p)}
}

type relayEP struct {
	peer [32]byte
	via  netip.AddrPort
}

func (e *relayEP) ClearSrc()           {}
func (e *relayEP) SrcToString() string { return "" }
func (e *relayEP) DstToString() string {
	return "relay:" + hex.EncodeToString(e.peer[:]) + "@" + e.via.String()
}
func (e *relayEP) DstIP() netip.Addr { return e.via.Addr() }
func (e *relayEP) SrcIP() netip.Addr { return netip.Addr{} }
func (e *relayEP) DstToBytes() []byte {
	ip := e.via.Addr().As4()
	p := e.via.Port()
	out := []byte{ip[0], ip[1], ip[2], ip[3], byte(p >> 8), byte(p)}
	out = append(out, e.peer[:]...)
	return out
}
