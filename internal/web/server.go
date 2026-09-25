package web

import (
	"embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"

	"keystone/internal/vncproxy"
)

//go:embed assets/index.html
var assets embed.FS

type Peer struct {
	Name     string `json:"name"`
	MeshIP   string `json:"mesh_ip"`
	OS       string `json:"os"`
	Online   bool   `json:"online"`
	VNC      bool   `json:"vnc"`
	Endpoint string `json:"endpoint"`
	Self     bool   `json:"self"`
}

type Host struct {
	Token    string
	Status   func() any
	Peers    func() []Peer
	Join     func() any
	DoFile   func(upstream, method, peer, path string, body io.Reader) (*http.Response, error)
	SetShare func(root string) error
	DialVNC  func(peer string) (net.Conn, error)
}

func Handler(h Host) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := assets.ReadFile("assets/index.html")
		page := strings.ReplaceAll(string(b), "KEYSTONE_LOCAL_TOKEN", h.Token)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, page)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if guard(h, w, r) {
			writeJSON(w, h.Status())
		}
	})
	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		if guard(h, w, r) {
			writeJSON(w, map[string]any{"peers": h.Peers()})
		}
	})
	mux.HandleFunc("/api/join", func(w http.ResponseWriter, r *http.Request) {
		if guard(h, w, r) {
			writeJSON(w, h.Join())
		}
	})
	mux.HandleFunc("/api/files", func(w http.ResponseWriter, r *http.Request) { h.proxyFile(w, r, "/list", http.MethodGet) })
	mux.HandleFunc("/api/download", func(w http.ResponseWriter, r *http.Request) { h.proxyFile(w, r, "/download", http.MethodGet) })
	mux.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) { h.proxyFile(w, r, "/upload", http.MethodPut) })
	mux.HandleFunc("/api/mkdir", func(w http.ResponseWriter, r *http.Request) { h.proxyFile(w, r, "/mkdir", http.MethodPost) })
	mux.HandleFunc("/api/delete", func(w http.ResponseWriter, r *http.Request) { h.proxyFile(w, r, "/delete", http.MethodDelete) })
	mux.HandleFunc("/api/share", func(w http.ResponseWriter, r *http.Request) {
		if !guard(h, w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if err := h.SetShare(r.URL.Query().Get("root")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/vnc", h.vnc)
	return mux
}

func guard(h Host, w http.ResponseWriter, r *http.Request) bool {
	if !sameHost(r) || r.Header.Get("X-Keystone") != h.Token || h.Token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func sameHost(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func (h Host) proxyFile(w http.ResponseWriter, r *http.Request, upstream, method string) {
	if !guard(h, w, r) {
		return
	}
	if r.Method != method {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	resp, err := h.DoFile(upstream, method, r.URL.Query().Get("peer"), r.URL.Query().Get("path"), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header, "Content-Type")
	copyHeader(w.Header(), resp.Header, "Content-Disposition")
	copyHeader(w.Header(), resp.Header, "Content-Length")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func copyHeader(dst, src http.Header, key string) {
	if v := src.Get(key); v != "" {
		dst.Set(key, v)
	}
}

func (h Host) vnc(w http.ResponseWriter, r *http.Request) {
	if !sameHost(r) || r.URL.Query().Get("token") != h.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	tcp, err := h.DialVNC(r.URL.Query().Get("peer"))
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, err.Error())
		return
	}
	stream := websocket.NetConn(r.Context(), ws, websocket.MessageBinary)
	_ = vncproxy.Bridge(stream, tcp, r.URL.Query().Get("password"))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
