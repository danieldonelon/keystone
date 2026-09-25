package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	Version      = "0.1.1"
	MeshCIDR     = "10.91.0.0/16"
	MeshFilePort = 7760
	MeshVNCPort  = 5900
	CoordPort    = 7707
	RelayPort    = 7708
	WebPort      = 8731
	WGPort       = 51830
	BypassMark   = 0x4b53
	BypassTable  = 9181
	LocalVNCPort = 5910
)

type File struct {
	Version        int    `json:"version"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	PrivateKey     string `json:"private_key"`
	PublicKey      string `json:"public_key"`
	MeshIP         string `json:"mesh_ip"`
	ListenPort     int    `json:"listen_port"`
	CoordinatorURL string `json:"coordinator_url"`
	Token          string `json:"token"`
	Pin            string `json:"pin"`
	NetworkSecret  string `json:"network_secret"`
	ShareRoot      string `json:"share_root"`
	WebPort        int    `json:"web_port"`
}

func Dir() string {
	if d := os.Getenv("KEYSTONE_HOME"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "Keystone")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "keystone")
}

func Path() string { return filepath.Join(Dir(), "config.json") }

func Load() (File, error) {
	b, err := os.ReadFile(Path())
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, err
	}
	return f, nil
}

func (f File) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writePrivate(Path(), b)
}

func StatePath() string   { return filepath.Join(Dir(), "state.json") }
func PeerCache() string   { return filepath.Join(Dir(), "peers.json") }
func CertPath() string    { return filepath.Join(Dir(), "coord.crt") }
func KeyPath() string     { return filepath.Join(Dir(), "coord.key") }
func RuntimePath() string { return filepath.Join(Dir(), "runtime.json") }

func writePrivate(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func WritePrivate(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writePrivate(path, b)
}

func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func NewSecret() (string, error) { return NewToken() }

func DefaultShareRoot() string {
	if s := os.Getenv("KEYSTONE_SHARE"); s != "" {
		return s
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func PrivateIPv4s() []netip.Addr {
	var out []netip.Addr
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip, ok := netip.AddrFromSlice(ipnet.IP.To4())
		if !ok || !ip.Is4() || !ip.IsPrivate() {
			continue
		}
		out = append(out, ip.Unmap())
	}
	return out
}

func BestAdvertiseIP() string {
	ips := PrivateIPv4s()
	if len(ips) == 0 {
		return "127.0.0.1"
	}
	return ips[0].String()
}

func GenerateCert(ips []net.IP) (pin string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "keystone", Organization: []string{"Keystone"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"keystone", "localhost"},
		IPAddresses:  append(ips, net.IPv4(127, 0, 0, 1)),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := WritePrivate(CertPath(), certPEM); err != nil {
		return "", err
	}
	if err := WritePrivate(KeyPath(), keyPEM); err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

func ServerTLS() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(CertPath(), KeyPath())
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

func PinOfCert(certPath string) (string, error) {
	b, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return "", fmt.Errorf("coordinator certificate is not PEM")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

func ClientTLS(pin string) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // identity is the pinned certificate, not a public CA
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("coordinator presented no certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(sum[:]) != pin {
				return fmt.Errorf("coordinator certificate does not match the saved pin")
			}
			return nil
		},
	}
}

func MeshIP(n int) string {
	return fmt.Sprintf("10.91.%d.%d", (n>>8)&0xff, n&0xff)
}
