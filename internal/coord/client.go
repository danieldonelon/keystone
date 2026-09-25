package coord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"

	"keystone/internal/bypass"
	"keystone/internal/config"
)

type Client struct {
	Base    string
	Token   string
	Pin     string
	HTTP    *http.Client
	IfIndex func() int
	Mark    func() int
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	dialer.Control = func(network, address string, raw syscall.RawConn) error {
		idx, mark := 0, 0
		if c.IfIndex != nil {
			idx = c.IfIndex()
		}
		if c.Mark != nil {
			mark = c.Mark()
		}
		return raw.Control(func(fd uintptr) { bypass.Protect4(fd, idx, mark) })
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
			TLSClientConfig: config.ClientTLS(c.Pin),
		},
	}
}

func (c *Client) Enroll(ctx context.Context, body any) (map[string]any, error) {
	return c.post(ctx, "/v1/enroll", body)
}

func (c *Client) Heartbeat(ctx context.Context, body any) (map[string]any, error) {
	return c.post(ctx, "/v1/heartbeat", body)
}

func (c *Client) post(ctx context.Context, path string, body any) (map[string]any, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("coordinator %s: %s", resp.Status, bytes.TrimSpace(data))
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
