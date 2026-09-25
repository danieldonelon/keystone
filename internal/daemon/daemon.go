package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"keystone/internal/bypass"
	"keystone/internal/config"
	"keystone/internal/coord"
	"keystone/internal/filesvc"
	"keystone/internal/mesh"
	"keystone/internal/relayproto"
	"keystone/internal/rfbsrv"
	"keystone/internal/web"
)

type runtimeInfo struct {
	PID   int    `json:"pid"`
	Web   string `json:"web"`
	Token string `json:"token"`
}

type wirePeer struct {
	Name      string   `json:"name"`
	PublicKey string   `json:"public_key"`
	MeshIP    string   `json:"mesh_ip"`
	Endpoint  string   `json:"endpoint"`
	LAN       []string `json:"lan"`
	OS        string   `json:"os"`
	VNC       bool     `json:"vnc"`
	Online    bool     `json:"online"`
}

type app struct {
	cfg       config.File
	m         *mesh.Mesh
	state     *coord.State
	choice    bypass.Choice
	choiceMu  sync.Mutex
	peersMu   sync.Mutex
	peers     []wirePeer
	relayPref map[string]bool
	share     atomic.Value
	uiToken   string
	vncLocal  string
	vncCmd    *exec.Cmd
	log       func(string, ...any)
}

func Up(ctx context.Context, openBrowser bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("no Keystone profile yet: run keystone init on the first computer, or keystone join on the others")
	}
	if cfg.ShareRoot == "" {
		cfg.ShareRoot = config.DefaultShareRoot()
	}
	a := &app{cfg: cfg, relayPref: map[string]bool{}, log: func(f string, args ...any) { fmt.Printf(f+"\n", args...) }}
	a.share.Store(cfg.ShareRoot)
	a.applyBypass()
	var tok [16]byte
	_, _ = rand.Read(tok[:])
	a.uiToken = hex.EncodeToString(tok[:])

	if cfg.Role == "coordinator" {
		st, err := coord.LoadState(config.StatePath())
		if err != nil {
			return err
		}
		a.state = st
		if err := a.serveCoordinator(ctx); err != nil {
			return err
		}
	}

	m, err := mesh.Start(cfg.PrivateKey, cfg.MeshIP, cfg.ListenPort)
	if err != nil {
		return fmt.Errorf("mesh: %w", err)
	}
	a.m = m
	defer m.Close()
	if cfg.ListenPort != m.Port() {
		cfg.ListenPort = m.Port()
		a.cfg.ListenPort = m.Port()
		_ = cfg.Save()
	}
	secret, err := hex.DecodeString(cfg.NetworkSecret)
	if err != nil {
		return fmt.Errorf("network secret: %w", err)
	}
	m.SetIdentity(secret, cfg.PublicKey)
	a.applyBypass()

	go a.watchBypass(ctx)
	go a.watchPeers(ctx)
	go a.watchRelay(ctx)
	a.serveFiles(ctx)
	a.serveScreen(ctx)

	webPort := cfg.WebPort
	if webPort == 0 {
		webPort = config.WebPort
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(webPort))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
	}
	page := "http://" + ln.Addr().String()
	_ = config.WritePrivate(config.RuntimePath(), mustJSON(runtimeInfo{PID: os.Getpid(), Web: page, Token: a.uiToken}))
	defer os.Remove(config.RuntimePath())

	srv := &http.Server{Handler: web.Handler(a.host()), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
		if a.vncCmd != nil && a.vncCmd.Process != nil {
			_ = a.vncCmd.Process.Kill()
		}
		_ = bypass.Release()
	}()
	a.log("Keystone is up at %s", page)
	a.log("%s", a.bypassLine())
	if openBrowser {
		openBrowserURL(page)
	}
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *app) host() web.Host {
	return web.Host{
		Token:    a.uiToken,
		Status:   a.status,
		Peers:    a.webPeers,
		Join:     a.joinInfo,
		DoFile:   a.doFile,
		SetShare: a.setShare,
		DialVNC:  a.dialVNC,
	}
}

func (a *app) status() any {
	a.choiceMu.Lock()
	ch := a.choice
	a.choiceMu.Unlock()
	return map[string]any{
		"name":     a.cfg.Name,
		"mesh_ip":  a.cfg.MeshIP,
		"role":     a.cfg.Role,
		"share":    a.cfg.ShareRoot,
		"port":     a.m.Port(),
		"vpn_up":   ch.VPNUp,
		"vpn_name": ch.VPNName,
		"physical": ch.PhysicalName,
		"bypass":   bypass.Describe(ch),
	}
}

func (a *app) joinInfo() any {
	if a.cfg.Role != "coordinator" {
		return map[string]any{"role": a.cfg.Role, "command": ""}
	}
	ip := config.BestAdvertiseIP()
	cmd := fmt.Sprintf("keystone join --name pi --coordinator https://%s:%d --token %s --pin %s", ip, config.CoordPort, a.cfg.Token, a.cfg.Pin)
	return map[string]any{"role": "coordinator", "command": cmd, "url": a.cfg.CoordinatorURL}
}

func (a *app) webPeers() []web.Peer {
	a.peersMu.Lock()
	defer a.peersMu.Unlock()
	out := []web.Peer{{
		Name: a.cfg.Name, MeshIP: a.cfg.MeshIP, OS: runtime.GOOS, Online: true, VNC: a.vncLocal != "", Self: true,
	}}
	for _, p := range a.peers {
		out = append(out, web.Peer{
			Name: p.Name, MeshIP: p.MeshIP, OS: p.OS, Online: p.Online, VNC: p.VNC, Endpoint: p.Endpoint,
		})
	}
	return out
}

func (a *app) setShare(root string) error {
	if root == "" {
		return errors.New("share folder is empty")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("share folder is not a directory")
	}
	a.cfg.ShareRoot = root
	a.share.Store(root)
	return a.cfg.Save()
}

func (a *app) shareRoot() string {
	if v := a.share.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return a.cfg.ShareRoot
}

func (a *app) doFile(upstream, method, peer, path string, body io.Reader) (*http.Response, error) {
	if peer == "" || peer == a.cfg.Name {
		req := httptest.NewRequest(method, upstream+"?path="+url.QueryEscape(path), body)
		req.Header.Set("Authorization", "Bearer "+a.cfg.NetworkSecret)
		rr := httptest.NewRecorder()
		filesvc.Handler(a.shareRoot, a.cfg.NetworkSecret).ServeHTTP(rr, req)
		return rr.Result(), nil
	}
	ip, err := a.meshIP(peer)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("http://%s:%d%s?path=%s", ip, config.MeshFilePort, upstream, url.QueryEscape(path))
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.NetworkSecret)
	client := &http.Client{Timeout: 0, Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return a.m.Dial(network, addr)
	}}}
	return client.Do(req)
}

func (a *app) dialVNC(peer string) (net.Conn, error) {
	if peer == "" || peer == a.cfg.Name {
		if a.vncLocal == "" {
			return nil, errors.New("this computer is not sharing a screen")
		}
		return net.Dial("tcp", a.vncLocal)
	}
	ip, err := a.meshIP(peer)
	if err != nil {
		return nil, err
	}
	return a.m.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(config.MeshVNCPort)))
}

func (a *app) meshIP(name string) (string, error) {
	a.peersMu.Lock()
	defer a.peersMu.Unlock()
	for _, p := range a.peers {
		if p.Name == name {
			return p.MeshIP, nil
		}
	}
	return "", fmt.Errorf("no machine named %s", name)
}

func (a *app) serveFiles(ctx context.Context) {
	ln, err := a.m.ListenTCP(config.MeshFilePort)
	if err != nil {
		a.log("file service: %v", err)
		return
	}
	srv := &http.Server{Handler: filesvc.Handler(a.shareRoot, a.cfg.NetworkSecret), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
}

func acceptRFB(ctx context.Context, ln net.Listener, cap rfbsrv.Capturer) {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go rfbsrv.Serve(c, cap)
	}
}

func portOpen(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func startLinuxVNC() *exec.Cmd {
	if path, err := exec.LookPath("wayvnc"); err == nil {
		cmd := exec.Command(path, "127.0.0.1", "5900")
		if err := cmd.Start(); err == nil {
			return cmd
		}
	}
	if path, err := exec.LookPath("x11vnc"); err == nil {
		cmd := exec.Command(path, "-localhost", "-rfbport", "5900", "-forever", "-shared", "-nopw")
		if err := cmd.Start(); err == nil {
			return cmd
		}
	}
	return nil
}

func proxyTCP(addr string, c net.Conn) {
	defer c.Close()
	d, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return
	}
	defer d.Close()
	go io.Copy(d, c)
	_, _ = io.Copy(c, d)
}

func (a *app) serveCoordinator(ctx context.Context) error {
	secret, err := hex.DecodeString(a.cfg.NetworkSecret)
	if err != nil {
		return err
	}
	cs := &coord.Server{State: a.state, Token: a.cfg.Token, Secret: secret}
	tlsCfg, err := config.ServerTLS()
	if err != nil {
		return err
	}
	raw, err := net.Listen("tcp", ":"+strconv.Itoa(config.CoordPort))
	if err != nil {
		return fmt.Errorf("coordinator: %w", err)
	}
	ln := tls.NewListener(&pinListen{Listener: raw, pin: a.pin}, tlsCfg)
	srv := &http.Server{Handler: cs.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{Port: config.RelayPort})
	if err != nil {
		return fmt.Errorf("relay: %w", err)
	}
	a.pinPacket(udp)
	go cs.ServeRelay(udp)
	go func() {
		<-ctx.Done()
		_ = srv.Close()
		_ = udp.Close()
	}()
	if _, ok := a.state.Get(a.cfg.PublicKey); !ok {
		_, _ = a.state.Allocate(a.cfg.PublicKey, a.cfg.Name, runtime.GOOS, a.cfg.ListenPort, nil, runtime.GOOS == "windows")
	}
	return nil
}

type pinListen struct {
	net.Listener
	pin func() (int, int)
}

func (p *pinListen) Accept() (net.Conn, error) {
	c, err := p.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if sc, ok := c.(interface {
		SyscallConn() (syscall.RawConn, error)
	}); ok {
		raw, err := sc.SyscallConn()
		if err == nil {
			idx, mark := p.pin()
			_ = raw.Control(func(fd uintptr) { bypass.Protect4(fd, idx, mark) })
		}
	}
	return c, nil
}

func (a *app) pin() (int, int) {
	a.choiceMu.Lock()
	defer a.choiceMu.Unlock()
	if !a.choice.VPNUp {
		return 0, 0
	}
	mark := 0
	if runtime.GOOS == "linux" {
		mark = config.BypassMark
	}
	return a.choice.PhysicalIndex, mark
}

func (a *app) pinPacket(c *net.UDPConn) {
	raw, err := c.SyscallConn()
	if err != nil {
		return
	}
	idx, mark := a.pin()
	_ = raw.Control(func(fd uintptr) { bypass.Protect4(fd, idx, mark) })
}

func (a *app) applyBypass() {
	ch, err := bypass.Snapshot()
	if err != nil {
		a.log("route lookup: %v", err)
		return
	}
	if err := bypass.Apply(ch); err != nil {
		a.log("bypass: %v", err)
	}
	a.choiceMu.Lock()
	a.choice = ch
	a.choiceMu.Unlock()
	idx, mark := a.pin()
	if a.m != nil {
		a.m.Bind().SetPin(idx, mark)
	}
}

func (a *app) bypassLine() string {
	a.choiceMu.Lock()
	defer a.choiceMu.Unlock()
	return bypass.Describe(a.choice)
}

func (a *app) watchBypass(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.applyBypass()
		}
	}
}

func (a *app) watchPeers(ctx context.Context) {
	a.refresh(ctx)
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.refresh(ctx)
			a.discover()
		}
	}
}

func (a *app) refresh(ctx context.Context) {
	if a.cfg.Role == "coordinator" && a.state != nil {
		_, _ = a.state.Touch(a.cfg.PublicKey, a.m.Port(), runtime.GOOS, lanAddrs(a.m.Port()), a.vncLocal != "")
		raw, _ := json.Marshal(coordPeers(a.state, a.cfg.PublicKey))
		var peers []wirePeer
		_ = json.Unmarshal(raw, &peers)
		a.installPeers(peers)
		return
	}
	c := &coord.Client{
		Base: a.cfg.CoordinatorURL, Token: a.cfg.Token, Pin: a.cfg.Pin,
		IfIndex: func() int { i, _ := a.pin(); return i },
		Mark:    func() int { _, m := a.pin(); return m },
	}
	resp, err := c.Heartbeat(ctx, map[string]any{
		"public_key": a.cfg.PublicKey, "listen_port": a.m.Port(), "os": runtime.GOOS,
		"lan": config.PrivateIPv4s(), "vnc": a.vncLocal != "",
	})
	if err != nil {
		a.log("coordinator: %v", err)
		return
	}
	b, _ := json.Marshal(resp["peers"])
	var peers []wirePeer
	if err := json.Unmarshal(b, &peers); err != nil {
		return
	}
	_ = config.WritePrivate(config.PeerCache(), b)
	a.installPeers(peers)
}

func coordPeers(st *coord.State, self string) []wirePeer {
	// Reuse the server's filtering by constructing a temporary server view via JSON.
	s := &coord.Server{State: st}
	b, _ := json.Marshal(s.PeersFor(self))
	var peers []wirePeer
	_ = json.Unmarshal(b, &peers)
	return peers
}

func (a *app) installPeers(in []wirePeer) {
	nets := localNets()
	relay := relayAddr(a.cfg.CoordinatorURL)
	var mp []mesh.Peer
	for _, p := range in {
		direct := pickDirect(p, nets)
		a.peersMu.Lock()
		pref := a.relayPref[p.PublicKey]
		a.peersMu.Unlock()
		useRelay := pref || (direct == "" && relay != "")
		mp = append(mp, mesh.Peer{
			Name: p.Name, PublicKey: p.PublicKey, MeshIP: p.MeshIP,
			Endpoint: direct, Relay: relay, UseRelay: useRelay,
		})
		if direct != "" {
			p.Endpoint = direct
		}
	}
	if err := a.m.SetPeers(mp); err != nil {
		a.log("peers: %v", err)
	}
	a.peersMu.Lock()
	a.peers = in
	a.peersMu.Unlock()
}

func (a *app) discover() {
	if a.cfg.Role == "coordinator" {
		return
	}
	secret, err := hex.DecodeString(a.cfg.NetworkSecret)
	if err != nil {
		return
	}
	self, ok := relayproto.DecodeKey(a.cfg.PublicKey)
	if !ok {
		return
	}
	ap, err := netip.ParseAddrPort(relayAddr(a.cfg.CoordinatorURL))
	if err != nil {
		return
	}
	_ = a.m.Bind().WriteControl(relayproto.SealDisc(secret, self, uint16(a.m.Port())), ap)
}

func (a *app) watchRelay(ctx context.Context) {
	t := time.NewTicker(12 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if a.m == nil {
				continue
			}
			hs := a.m.Handshakes()
			a.peersMu.Lock()
			cur := append([]wirePeer(nil), a.peers...)
			a.peersMu.Unlock()
			for _, p := range cur {
				tm, ok := hs[p.PublicKey]
				if ok && time.Since(tm) < 40*time.Second {
					continue
				}
				a.peersMu.Lock()
				if a.relayPref == nil {
					a.relayPref = map[string]bool{}
				}
				a.relayPref[p.PublicKey] = true
				a.peersMu.Unlock()
				_ = a.m.PreferRelay(p.PublicKey, true)
			}
		}
	}
}

func lanAddrs(port int) []string {
	var out []string
	for _, ip := range config.PrivateIPv4s() {
		out = append(out, netip.AddrPortFrom(ip, uint16(port)).String())
	}
	return out
}

func localNets() []netip.Prefix {
	var out []netip.Prefix
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP.To4())
			if !ok {
				continue
			}
			ones, _ := ipn.Mask.Size()
			p, err := ip.Unmap().Prefix(ones)
			if err == nil {
				out = append(out, p)
			}
		}
	}
	return out
}

func pickDirect(p wirePeer, nets []netip.Prefix) string {
	cands := append([]string{}, p.LAN...)
	if p.Endpoint != "" {
		cands = append(cands, p.Endpoint)
	}
	public := ""
	for _, ep := range cands {
		host, _, err := net.SplitHostPort(ep)
		if err != nil {
			continue
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			continue
		}
		for _, n := range nets {
			if n.Contains(ip) {
				return ep
			}
		}
		if ip.IsGlobalUnicast() && !ip.IsPrivate() && public == "" {
			public = ep
		}
	}
	return public
}

func relayAddr(coordinatorURL string) string {
	u, err := url.Parse(coordinatorURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return net.JoinHostPort(u.Hostname(), strconv.Itoa(config.RelayPort))
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}

func openBrowserURL(raw string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	_ = cmd.Start()
}

func Doctor(ctx context.Context) error {
	ch, err := bypass.Snapshot()
	if err != nil {
		return err
	}
	fmt.Println(bypass.Describe(ch))
	plain, err1 := bypass.MappedAddr(ctx, 0, 0)
	idx, mark := 0, 0
	if ch.VPNUp {
		idx = ch.PhysicalIndex
		if runtime.GOOS == "linux" {
			mark = config.BypassMark
		}
	}
	pinned, err2 := bypass.MappedAddr(ctx, idx, mark)
	fmt.Printf("UDP without a pin: %s", dash(plain, err1))
	fmt.Println()
	fmt.Printf("UDP pinned for Keystone: %s", dash(pinned, err2))
	fmt.Println()
	if err1 == nil && err2 == nil && plain != pinned {
		fmt.Println("Bypass is working: Keystone can use a different path from the VPN.")
	} else if !ch.VPNUp {
		fmt.Println("ExpressVPN is not taking the default route right now, so no bypass is required.")
	} else if err2 != nil {
		fmt.Println("Could not confirm the bypass. Allow Keystone through the firewall, or exclude it in ExpressVPN split tunneling.")
	}
	return nil
}

func dash(ip string, err error) string {
	if err != nil {
		return "unavailable (" + err.Error() + ")"
	}
	return ip
}

func Status() error {
	b, err := os.ReadFile(config.RuntimePath())
	if err != nil {
		cfg, cerr := config.Load()
		if cerr != nil {
			fmt.Println("Keystone is not running and has no profile.")
			return nil
		}
		fmt.Printf("Keystone is not running. Profile %s (%s) is ready.\n", cfg.Name, cfg.MeshIP)
		return nil
	}
	var rt runtimeInfo
	if err := json.Unmarshal(b, &rt); err != nil {
		return err
	}
	req, _ := http.NewRequest(http.MethodGet, rt.Web+"/api/status", nil)
	req.Header.Set("X-Keystone", rt.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Keystone is not running.")
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var buf bytes.Buffer
	_ = json.Indent(&buf, body, "", "  ")
	fmt.Println(buf.String())
	fmt.Println(rt.Web)
	return nil
}
