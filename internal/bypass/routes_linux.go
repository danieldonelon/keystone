//go:build linux

package bypass

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"keystone/internal/config"
)

func Snapshot() (Choice, error) {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return Choice{}, err
	}
	lines := strings.Split(string(b), "\n")
	var routes []RouteInfo
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		dest, err1 := parseLE(f[1])
		gw, err2 := parseLE(f[2])
		mask, err3 := parseLE(f[7])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		bits := maskBits(mask)
		pref, err := dest.Prefix(bits)
		if err != nil {
			continue
		}
		ifi, err := net.InterfaceByName(f[0])
		idx, name := 0, f[0]
		if err == nil {
			idx = ifi.Index
		}
		metric, _ := strconv.Atoi(f[6])
		routes = append(routes, RouteInfo{IfIndex: idx, IfName: name, Prefix: pref, NextHop: gw, Metric: metric})
	}
	return Choose(routes), nil
}

func parseLE(s string) (netip.Addr, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 4 {
		return netip.Addr{}, fmt.Errorf("bad addr")
	}
	return netip.AddrFrom4([4]byte{b[3], b[2], b[1], b[0]}), nil
}

func maskBits(ip netip.Addr) int {
	a := ip.As4()
	n := 0
	for _, by := range a {
		for m := byte(0x80); m != 0; m >>= 1 {
			if by&m == 0 {
				return n
			}
			n++
		}
	}
	return n
}

func Apply(c Choice) error {
	if !c.VPNUp || c.PhysicalIndex == 0 || !c.Gateway.IsValid() {
		return Release()
	}
	table := strconv.Itoa(config.BypassTable)
	mark := strconv.Itoa(config.BypassMark)
	ifi, err := net.InterfaceByIndex(c.PhysicalIndex)
	if err != nil {
		return err
	}
	_ = exec.Command("ip", "route", "replace", "default", "via", c.Gateway.String(), "dev", ifi.Name, "table", table).Run()
	_ = exec.Command("ip", "rule", "del", "pref", "10").Run()
	if out, err := exec.Command("ip", "rule", "add", "pref", "10", "fwmark", mark, "lookup", table).CombinedOutput(); err != nil {
		return fmt.Errorf("ip rule: %v (%s)", err, out)
	}
	_ = os.WriteFile("/proc/sys/net/ipv4/conf/all/rp_filter", []byte("2\n"), 0o644)
	_ = os.WriteFile("/proc/sys/net/ipv4/conf/"+ifi.Name+"/rp_filter", []byte("2\n"), 0o644)
	return nil
}

func Release() error {
	_ = exec.Command("ip", "rule", "del", "pref", "10").Run()
	_ = exec.Command("ip", "route", "flush", "table", strconv.Itoa(config.BypassTable)).Run()
	return nil
}

func Describe(c Choice) string {
	if !c.VPNUp {
		return "No competing VPN route is active."
	}
	if c.PhysicalIndex == 0 {
		return fmt.Sprintf("%s is up, and Keystone could not find a physical adapter.", PrettyVPN(c.VPNName))
	}
	return fmt.Sprintf("%s is up. Marked Keystone packets use %s via %s.", PrettyVPN(c.VPNName), c.PhysicalName, c.Gateway)
}
