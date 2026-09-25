package bypass

import (
	"net/netip"
	"strings"
)

type RouteInfo struct {
	IfIndex int
	IfName  string
	Prefix  netip.Prefix
	NextHop netip.Addr
	Metric  int
}

type Choice struct {
	VPNUp         bool
	VPNName       string
	VPNIndex      int
	PhysicalName  string
	PhysicalIndex int
	Gateway       netip.Addr
}

func Choose(routes []RouteInfo) Choice {
	var c Choice
	vpnIdx := map[int]string{}
	for _, r := range routes {
		if isSplitDefault(r) || nameLooksVirtual(r.IfName) && isDefault(r) {
			vpnIdx[r.IfIndex] = r.IfName
			if !c.VPNUp || nameLooksVirtual(r.IfName) {
				c.VPNUp = true
				c.VPNName = r.IfName
				c.VPNIndex = r.IfIndex
			}
		}
	}
	bestMetric := int(^uint(0) >> 1)
	for _, r := range routes {
		if !isDefault(r) || !r.NextHop.IsValid() || r.NextHop.IsUnspecified() {
			continue
		}
		if _, virtual := vpnIdx[r.IfIndex]; virtual || nameLooksVirtual(r.IfName) {
			continue
		}
		if r.Metric < bestMetric {
			bestMetric = r.Metric
			c.PhysicalIndex = r.IfIndex
			c.PhysicalName = r.IfName
			c.Gateway = r.NextHop
		}
	}
	if !c.VPNUp {
		return Choice{}
	}
	return c
}

func isDefault(r RouteInfo) bool {
	return r.Prefix.IsValid() && r.Prefix.Bits() == 0 && r.Prefix.Addr().Is4() && r.Prefix.Addr().IsUnspecified()
}

func isSplitDefault(r RouteInfo) bool {
	if !r.Prefix.IsValid() || r.Prefix.Bits() != 1 || !r.Prefix.Addr().Is4() {
		return false
	}
	a := r.Prefix.Addr().As4()
	return a == [4]byte{} || a == [4]byte{128, 0, 0, 0}
}

func PrettyVPN(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "expressvpn"):
		return "ExpressVPN"
	case strings.Contains(n, "nord"):
		return "NordVPN"
	case strings.Contains(n, "tailscale"):
		return "Tailscale"
	case strings.Contains(n, "proton"):
		return "Proton VPN"
	case strings.Contains(n, "mullvad"):
		return "Mullvad"
	case strings.Contains(n, "surfshark"):
		return "Surfshark"
	default:
		return name
	}
}

func nameLooksVirtual(name string) bool {
	n := strings.ToLower(name)
	hints := []string{
		"expressvpn", "lightway", "nordlynx", "nordvpn", "tailscale", "wireguard",
		"wintun", "openvpn", "anyconnect", "proton", "surfshark", "mullvad",
		"zerotier", "tunnelbear", "private internet", "softether", "hamachi",
		"tap-windows", "vpn",
	}
	for _, h := range hints {
		if strings.Contains(n, h) {
			return true
		}
	}
	return false
}
