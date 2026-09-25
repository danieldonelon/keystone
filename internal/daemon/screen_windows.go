//go:build windows

package daemon

import (
	"context"
	"net"
	"strconv"

	"keystone/internal/config"
	"keystone/internal/rfbsrv"
)

func (a *app) serveScreen(ctx context.Context) {
	desk := rfbsrv.NewDesktop()
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(config.LocalVNCPort))
	if err != nil {
		a.log("screen: %v", err)
		return
	}
	a.vncLocal = ln.Addr().String()
	go acceptRFB(ctx, ln, desk)
	mln, err := a.m.ListenTCP(config.MeshVNCPort)
	if err != nil {
		a.log("mesh screen: %v", err)
		return
	}
	go acceptRFB(ctx, mln, desk)
}
