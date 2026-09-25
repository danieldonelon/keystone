//go:build windows

package bypass

import (
	"fmt"
	"net"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

func Snapshot() (Choice, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_INET, &table); err != nil {
		return Choice{}, err
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	names := map[int]string{}
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		names[ifi.Index] = ifi.Name
	}
	var routes []RouteInfo
	for _, row := range table.Rows() {
		dest, ok := addrFromSockaddr(&row.DestinationPrefix.Prefix)
		if !ok {
			continue
		}
		pref, err := dest.Prefix(int(row.DestinationPrefix.PrefixLength))
		if err != nil {
			continue
		}
		hop, _ := addrFromSockaddr(&row.NextHop)
		idx := int(row.InterfaceIndex)
		routes = append(routes, RouteInfo{
			IfIndex: idx,
			IfName:  names[idx],
			Prefix:  pref,
			NextHop: hop,
			Metric:  int(row.Metric),
		})
	}
	return Choose(routes), nil
}

func addrFromSockaddr(sa *windows.RawSockaddrInet) (netip.Addr, bool) {
	if sa.Family == windows.AF_INET {
		raw := (*windows.RawSockaddrInet4)(unsafe.Pointer(sa))
		return netip.AddrFrom4(raw.Addr), true
	}
	return netip.Addr{}, false
}

func Apply(c Choice) error { return nil }

func Release() error { return nil }

func Describe(c Choice) string {
	if !c.VPNUp {
		return "No competing VPN route is active."
	}
	if c.PhysicalIndex == 0 {
		return fmt.Sprintf("%s is up, and Keystone could not find a physical adapter to pin to.", PrettyVPN(c.VPNName))
	}
	gw := ""
	if c.Gateway.IsValid() {
		gw = " via " + c.Gateway.String()
	}
	return fmt.Sprintf("%s is up. Keystone sockets stay on %s%s.", PrettyVPN(c.VPNName), c.PhysicalName, gw)
}
