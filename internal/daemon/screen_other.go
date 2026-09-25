//go:build !windows

package daemon

import (
	"context"
	"time"

	"keystone/internal/config"
)

func (a *app) serveScreen(ctx context.Context) {
	addr := "127.0.0.1:5900"
	if !portOpen(addr) {
		a.vncCmd = startLinuxVNC()
		time.Sleep(300 * time.Millisecond)
	}
	if portOpen(addr) {
		a.vncLocal = addr
	}
	mln, err := a.m.ListenTCP(config.MeshVNCPort)
	if err != nil {
		a.log("mesh screen: %v", err)
		return
	}
	go func() {
		<-ctx.Done()
		_ = mln.Close()
	}()
	go func() {
		for {
			c, err := mln.Accept()
			if err != nil {
				return
			}
			go proxyTCP(addr, c)
		}
	}()
}
