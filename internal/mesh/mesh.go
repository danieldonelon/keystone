package mesh

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"keystone/internal/identity"
	"keystone/internal/relayproto"
	"keystone/internal/wgbind"
)

type Peer struct {
	Name      string
	PublicKey string
	MeshIP    string
	Endpoint  string
	Relay     string
	UseRelay  bool
}

type Mesh struct {
	dev   *device.Device
	tun   deviceTun
	net   *netstack.Net
	bind  *wgbind.Bind
	ip    netip.Addr
	priv  string
	mu    sync.Mutex
	peers []Peer
	port  int
}

type deviceTun interface {
	Close() error
}

func Start(privateHex, localIP string, listenPort int) (*Mesh, error) {
	priv, err := identity.Parse(privateHex)
	if err != nil {
		return nil, err
	}
	pub, err := priv.Public()
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(localIP)
	if err != nil {
		return nil, err
	}
	tunDev, tnet, err := netstack.CreateNetTUN([]netip.Addr{ip}, nil, 1280)
	if err != nil {
		return nil, err
	}
	bind := wgbind.New()
	var self [32]byte
	copy(self[:], pub.Bytes())
	dev := device.NewDevice(tunDev, bind, device.NewLogger(device.LogLevelError, "keystone "))
	m := &Mesh{dev: dev, tun: tunDev, net: tnet, bind: bind, ip: ip, priv: privateHex, port: listenPort}
	if err := dev.IpcSet(fmt.Sprintf("private_key=%s\nlisten_port=%d\n", privateHex, listenPort)); err != nil {
		dev.Close()
		return nil, err
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, err
	}
	m.port = bind.Port()
	_ = self
	return m, nil
}

func (m *Mesh) Close() {
	m.dev.Close()
}

func (m *Mesh) Net() *netstack.Net { return m.net }
func (m *Mesh) Bind() *wgbind.Bind { return m.bind }
func (m *Mesh) Port() int          { return m.port }
func (m *Mesh) IP() netip.Addr     { return m.ip }

func (m *Mesh) SetIdentity(secret []byte, selfPub string) {
	key, ok := relayproto.DecodeKey(selfPub)
	if !ok {
		return
	}
	m.bind.SetIdentity(secret, key)
}

func (m *Mesh) SetPeers(peers []Peer) error {
	m.mu.Lock()
	m.peers = append([]Peer(nil), peers...)
	text := m.peerConfigLocked()
	m.mu.Unlock()
	return m.dev.IpcSet(text)
}

func (m *Mesh) peerConfigLocked() string {
	var b strings.Builder
	b.WriteString("replace_peers=true\n")
	for _, p := range m.peers {
		if p.PublicKey == "" || p.MeshIP == "" {
			continue
		}
		fmt.Fprintf(&b, "public_key=%s\n", p.PublicKey)
		ep := p.Endpoint
		if p.UseRelay && p.Relay != "" {
			ep = "relay:" + p.PublicKey + "@" + p.Relay
		}
		if ep != "" {
			fmt.Fprintf(&b, "endpoint=%s\n", ep)
		}
		b.WriteString("persistent_keepalive_interval=25\n")
		b.WriteString("replace_allowed_ips=true\n")
		fmt.Fprintf(&b, "allowed_ip=%s/32\n", p.MeshIP)
	}
	return b.String()
}

func (m *Mesh) PreferRelay(pub string, on bool) error {
	m.mu.Lock()
	changed := false
	for i := range m.peers {
		if m.peers[i].PublicKey == pub && m.peers[i].UseRelay != on {
			m.peers[i].UseRelay = on
			changed = true
		}
	}
	text := ""
	if changed {
		text = m.peerConfigLocked()
	}
	m.mu.Unlock()
	if !changed {
		return nil
	}
	return m.dev.IpcSet(text)
}

type Handshakes map[string]time.Time

func (m *Mesh) Handshakes() Handshakes {
	raw, err := m.dev.IpcGet()
	if err != nil {
		return nil
	}
	out := Handshakes{}
	var cur string
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "public_key":
			cur = v
		case "last_handshake_time_sec":
			if cur == "" || v == "0" {
				continue
			}
			var sec int64
			fmt.Sscanf(v, "%d", &sec)
			if sec > 0 {
				out[cur] = time.Unix(sec, 0)
			}
		}
	}
	return out
}

func (m *Mesh) ListenTCP(port int) (net.Listener, error) {
	return m.net.ListenTCPAddrPort(netip.AddrPortFrom(m.ip, uint16(port)))
}

func (m *Mesh) Dial(network, address string) (net.Conn, error) {
	return m.net.Dial(network, address)
}
