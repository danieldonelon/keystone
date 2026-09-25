package bypass

import (
	"net/netip"
	"testing"
)

func TestChooseExpressVPNShape(t *testing.T) {
	p := func(s string) netip.Prefix { return netip.MustParsePrefix(s) }
	a := func(s string) netip.Addr { return netip.MustParseAddr(s) }
	got := Choose([]RouteInfo{
		{IfIndex: 5, IfName: "Wi-Fi", Prefix: p("0.0.0.0/0"), NextHop: a("192.168.0.1"), Metric: 0},
		{IfIndex: 34, IfName: "ExpressVPN Tunnel", Prefix: p("0.0.0.0/1"), NextHop: a("100.64.100.5"), Metric: 0},
		{IfIndex: 34, IfName: "ExpressVPN Tunnel", Prefix: p("128.0.0.0/1"), NextHop: a("100.64.100.5"), Metric: 0},
	})
	if !got.VPNUp || got.PhysicalIndex != 5 || got.PhysicalName != "Wi-Fi" {
		t.Fatalf("choice = %+v", got)
	}
	if got.Gateway.String() != "192.168.0.1" {
		t.Fatalf("gateway %s", got.Gateway)
	}
}

func TestChooseNoVPN(t *testing.T) {
	got := Choose([]RouteInfo{{
		IfIndex: 5, IfName: "Wi-Fi",
		Prefix:  netip.MustParsePrefix("0.0.0.0/0"),
		NextHop: netip.MustParseAddr("192.168.0.1"),
	}})
	if got.VPNUp || got.PhysicalIndex != 0 {
		t.Fatalf("choice = %+v", got)
	}
}
