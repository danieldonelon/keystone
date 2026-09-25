//go:build !windows && !linux

package bypass

import "fmt"

func Snapshot() (Choice, error) { return Choice{}, nil }

func Apply(c Choice) error { return nil }

func Release() error { return nil }

func Describe(c Choice) string {
	if !c.VPNUp {
		return "No competing VPN route is active."
	}
	return fmt.Sprintf("%s is up.", PrettyVPN(c.VPNName))
}
