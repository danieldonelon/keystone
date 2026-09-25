package coord

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"keystone/internal/config"
)

type Node struct {
	Name       string    `json:"name"`
	PublicKey  string    `json:"public_key"`
	MeshIP     string    `json:"mesh_ip"`
	ListenPort int       `json:"listen_port"`
	OS         string    `json:"os"`
	LAN        []string  `json:"lan"`
	Endpoint   string    `json:"endpoint"`
	VNC        bool      `json:"vnc"`
	LastSeen   time.Time `json:"last_seen"`
}

type State struct {
	mu    sync.Mutex
	path  string
	Nodes map[string]Node `json:"nodes"`
}

func LoadState(path string) (*State, error) {
	s := &State{path: path, Nodes: map[string]Node{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	if s.Nodes == nil {
		s.Nodes = map[string]Node{}
	}
	s.path = path
	return s, nil
}

func (s *State) saveLocked() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return config.WritePrivate(s.path, append(b, '\n'))
}

func (s *State) Upsert(n Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n.LastSeen.IsZero() {
		n.LastSeen = time.Now()
	}
	s.Nodes[n.PublicKey] = n
	return s.saveLocked()
}

func (s *State) SetEndpoint(pub, endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.Nodes[pub]
	if !ok {
		return os.ErrNotExist
	}
	n.Endpoint = endpoint
	n.LastSeen = time.Now()
	s.Nodes[pub] = n
	return s.saveLocked()
}

func (s *State) Touch(pub string, port int, osName string, lan []string, vnc bool) (Node, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.Nodes[pub]
	if !ok {
		return Node{}, false
	}
	n.ListenPort = port
	if osName != "" {
		n.OS = osName
	}
	n.LAN = lan
	n.VNC = vnc
	n.LastSeen = time.Now()
	s.Nodes[pub] = n
	_ = s.saveLocked()
	return n, true
}

func (s *State) Allocate(pub, name, osName string, port int, lan []string, vnc bool) (Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.Nodes[pub]; ok {
		n.Name = name
		n.ListenPort = port
		n.OS = osName
		n.LAN = lan
		n.VNC = vnc
		n.LastSeen = time.Now()
		s.Nodes[pub] = n
		return n, s.saveLocked()
	}
	used := map[string]bool{}
	for _, n := range s.Nodes {
		used[n.MeshIP] = true
	}
	ip := ""
	for i := 1; i < 65000; i++ {
		cand := config.MeshIP(i)
		if !used[cand] {
			ip = cand
			break
		}
	}
	if ip == "" {
		return Node{}, os.ErrInvalid
	}
	n := Node{
		Name: name, PublicKey: pub, MeshIP: ip, ListenPort: port,
		OS: osName, LAN: lan, VNC: vnc, LastSeen: time.Now(),
	}
	s.Nodes[pub] = n
	return n, s.saveLocked()
}

func (s *State) List() []Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Node, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		out = append(out, n)
	}
	return out
}

func (s *State) Get(pub string) (Node, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.Nodes[pub]
	return n, ok
}
