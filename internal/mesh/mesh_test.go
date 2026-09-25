package mesh

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"keystone/internal/filesvc"
	"keystone/internal/identity"
)

func TestTwoNodesTransfer(t *testing.T) {
	aKey, err := identity.NewPrivate()
	if err != nil {
		t.Fatal(err)
	}
	bKey, err := identity.NewPrivate()
	if err != nil {
		t.Fatal(err)
	}
	aPub, _ := aKey.Public()
	bPub, _ := bKey.Public()

	a, err := Start(aKey.Hex(), "10.91.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Start(bKey.Hex(), "10.91.0.2", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := a.SetPeers([]Peer{{PublicKey: bPub.Hex(), MeshIP: "10.91.0.2", Endpoint: "127.0.0.1:" + itoa(b.Port())}}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetPeers([]Peer{{PublicKey: aPub.Hex(), MeshIP: "10.91.0.1", Endpoint: "127.0.0.1:" + itoa(a.Port())}}); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	ln, err := b.ListenTCP(7760)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: filesvc.Handler(func() string { return root }, "secret")}
	go srv.Serve(ln)
	defer srv.Close()

	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return a.Dial(network, addr)
		},
	}}
	resp, err := client.Get("http://10.91.0.2:7760/list?path=")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPut, "http://10.91.0.2:7760/upload?path=hello.txt", strings.NewReader("hello from keystone"))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("upload status %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, "http://10.91.0.2:7760/download?path=hello.txt", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from keystone" {
		t.Fatalf("body %q status %d", body, resp.StatusCode)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
