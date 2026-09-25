package coord

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"keystone/internal/config"
	"keystone/internal/relayproto"
)

type Server struct {
	State  *State
	Token  string
	Secret []byte
	HTTP   *http.Server
}

type peerView struct {
	Name      string   `json:"name"`
	PublicKey string   `json:"public_key"`
	MeshIP    string   `json:"mesh_ip"`
	Endpoint  string   `json:"endpoint"`
	LAN       []string `json:"lan"`
	OS        string   `json:"os"`
	VNC       bool     `json:"vnc"`
	Online    bool     `json:"online"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/enroll", s.enroll)
	mux.HandleFunc("/v1/heartbeat", s.heartbeat)
	mux.HandleFunc("/v1/peers", s.peers)
	return mux
}

func (s *Server) authorize(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(got) != len(s.Token) || s.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Name       string   `json:"name"`
		PublicKey  string   `json:"public_key"`
		ListenPort int      `json:"listen_port"`
		OS         string   `json:"os"`
		LAN        []string `json:"lan"`
		VNC        bool     `json:"vnc"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := hex.DecodeString(req.PublicKey); err != nil || len(req.PublicKey) != 64 {
		http.Error(w, "bad public key", http.StatusBadRequest)
		return
	}
	n, err := s.State.Allocate(req.PublicKey, req.Name, req.OS, req.ListenPort, withPort(req.LAN, req.ListenPort), req.VNC)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"mesh_ip":        n.MeshIP,
		"mesh_cidr":      config.MeshCIDR,
		"network_secret": hex.EncodeToString(s.Secret),
		"relay_port":     config.RelayPort,
		"peers":          s.views(req.PublicKey),
	})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		PublicKey  string   `json:"public_key"`
		ListenPort int      `json:"listen_port"`
		OS         string   `json:"os"`
		LAN        []string `json:"lan"`
		VNC        bool     `json:"vnc"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok := s.State.Touch(req.PublicKey, req.ListenPort, req.OS, withPort(req.LAN, req.ListenPort), req.VNC); !ok {
		http.Error(w, "unknown node", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"peers": s.views(req.PublicKey)})
}

func (s *Server) peers(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"peers": s.views(r.URL.Query().Get("self"))})
}

func (s *Server) PeersFor(self string) any { return s.views(self) }

func (s *Server) views(self string) []peerView {
	nodes := s.State.List()
	out := make([]peerView, 0, len(nodes))
	now := time.Now()
	for _, n := range nodes {
		if n.PublicKey == self {
			continue
		}
		ep := n.Endpoint
		if host, _, err := net.SplitHostPort(ep); err == nil {
			if ip, err := netip.ParseAddr(host); err == nil && ip.IsLoopback() {
				ep = ""
			}
		}
		out = append(out, peerView{
			Name: n.Name, PublicKey: n.PublicKey, MeshIP: n.MeshIP,
			Endpoint: ep, LAN: n.LAN, OS: n.OS, VNC: n.VNC,
			Online: now.Sub(n.LastSeen) < 45*time.Second,
		})
	}
	return out
}

func (s *Server) ServeRelay(conn *net.UDPConn) {
	buf := make([]byte, 65535)
	for {
		n, addr, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		pkt := append([]byte(nil), buf[:n]...)
		if len(pkt) >= 4 && string(pkt[:4]) == relayproto.MagicDisc {
			pub, _, ok := relayproto.OpenDisc(s.Secret, pkt, time.Now())
			if !ok || addr.Addr().IsLoopback() {
				continue
			}
			_ = s.State.SetEndpoint(relayproto.EncodeKey(pub), addr.String())
			continue
		}
		src, dst, _, ok := relayproto.OpenRelay(s.Secret, pkt)
		if !ok {
			continue
		}
		if _, exists := s.State.Get(relayproto.EncodeKey(src)); !exists {
			continue
		}
		if !addr.Addr().IsLoopback() {
			_ = s.State.SetEndpoint(relayproto.EncodeKey(src), addr.String())
		}
		dest, ok := s.State.Get(relayproto.EncodeKey(dst))
		if !ok || dest.Endpoint == "" {
			continue
		}
		to, err := netip.ParseAddrPort(dest.Endpoint)
		if err != nil {
			continue
		}
		_, _ = conn.WriteToUDPAddrPort(pkt, to)
	}
}

func withPort(ips []string, port int) []string {
	if port <= 0 {
		return ips
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if _, _, err := net.SplitHostPort(ip); err == nil {
			out = append(out, ip)
			continue
		}
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		out = append(out, netip.AddrPortFrom(addr, uint16(port)).String())
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
