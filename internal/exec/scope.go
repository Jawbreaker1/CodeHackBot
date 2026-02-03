package exec

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

var (
	ipv4CIDRPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}\b`)
	ipv4Pattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
)

var internalCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
}

type scopeEntries struct {
	ips      []net.IP
	nets     []*net.IPNet
	literals []string
}

func (r *Runner) validateScope(command string, args []string) error {
	if len(r.ScopeNetworks) == 0 && len(r.ScopeTargets) == 0 && len(r.ScopeDenyTargets) == 0 {
		return nil
	}

	allowEntries := parseScopeEntries(append(r.ScopeNetworks, r.ScopeTargets...))
	denyEntries := parseScopeEntries(r.ScopeDenyTargets)
	literals := append([]string{}, allowEntries.literals...)
	literals = append(literals, denyEntries.literals...)

	targets := extractTargets(command, args, literals)
	if len(targets) == 0 {
		return nil
	}

	allowEnforced := len(r.ScopeNetworks) > 0 || len(r.ScopeTargets) > 0
	violations := []string{}

	for _, raw := range targets {
		target := strings.ToLower(raw)
		ip, cidr, isLiteral := parseTarget(target, allowEntries.literals, denyEntries.literals)

		if isLiteral {
			if containsLiteral(denyEntries.literals, target) {
				violations = append(violations, fmt.Sprintf("denied target %s", raw))
				continue
			}
			if allowEnforced && !containsLiteral(allowEntries.literals, target) {
				violations = append(violations, fmt.Sprintf("out of scope target %s", raw))
			}
			continue
		}

		if ip == nil && cidr == nil {
			continue
		}

		if ip != nil && matchIP(ip, denyEntries) {
			violations = append(violations, fmt.Sprintf("denied target %s", raw))
			continue
		}
		if cidr != nil && matchCIDRDeny(cidr, denyEntries) {
			violations = append(violations, fmt.Sprintf("denied target %s", raw))
			continue
		}

		if allowEnforced {
			allowed := false
			if ip != nil {
				allowed = matchIP(ip, allowEntries)
			} else if cidr != nil {
				allowed = matchCIDRAllow(cidr, allowEntries)
			}
			if !allowed {
				violations = append(violations, fmt.Sprintf("out of scope target %s", raw))
			}
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("scope violation: %s", strings.Join(violations, "; "))
	}
	return nil
}

func parseScopeEntries(entries []string) scopeEntries {
	result := scopeEntries{}
	for _, entry := range expandAliases(entries) {
		clean := strings.ToLower(strings.TrimSpace(entry))
		if clean == "" {
			continue
		}
		if strings.Contains(clean, "/") {
			if _, network, err := net.ParseCIDR(clean); err == nil {
				result.nets = append(result.nets, network)
				continue
			}
		}
		if ip := net.ParseIP(clean); ip != nil {
			result.ips = append(result.ips, ip)
			continue
		}
		result.literals = append(result.literals, clean)
	}
	return result
}

func expandAliases(entries []string) []string {
	out := []string{}
	for _, entry := range entries {
		clean := strings.ToLower(strings.TrimSpace(entry))
		switch clean {
		case "internal", "private":
			out = append(out, internalCIDRs...)
		case "loopback", "local":
			out = append(out, "127.0.0.0/8")
		case "linklocal":
			out = append(out, "169.254.0.0/16")
		default:
			if entry != "" {
				out = append(out, entry)
			}
		}
	}
	return out
}

func extractTargets(command string, args []string, literals []string) []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		if value == "" {
			return
		}
		seen[value] = struct{}{}
	}

	candidates := append([]string{command}, args...)
	for _, token := range candidates {
		lower := strings.ToLower(token)
		if strings.Contains(lower, "localhost") {
			add("localhost")
		}
		for _, match := range ipv4CIDRPattern.FindAllString(token, -1) {
			add(match)
		}
		for _, match := range ipv4Pattern.FindAllString(token, -1) {
			add(match)
		}
		for _, literal := range literals {
			if literal != "" && strings.Contains(lower, literal) {
				add(literal)
			}
		}
	}

	targets := make([]string, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	return targets
}

func parseTarget(raw string, allowLiterals, denyLiterals []string) (net.IP, *net.IPNet, bool) {
	if containsLiteral(allowLiterals, raw) || containsLiteral(denyLiterals, raw) {
		return nil, nil, true
	}
	if raw == "localhost" {
		return net.ParseIP("127.0.0.1"), nil, false
	}
	if strings.Contains(raw, "/") {
		if _, network, err := net.ParseCIDR(raw); err == nil {
			return nil, network, false
		}
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ip, nil, false
	}
	return nil, nil, true
}

func containsLiteral(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

func matchIP(ip net.IP, entries scopeEntries) bool {
	for _, allowed := range entries.ips {
		if allowed.Equal(ip) {
			return true
		}
	}
	for _, network := range entries.nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func matchCIDRAllow(target *net.IPNet, entries scopeEntries) bool {
	ones, bits := target.Mask.Size()
	if bits > 0 && ones == bits {
		if matchIP(target.IP, entries) {
			return true
		}
	}
	for _, network := range entries.nets {
		if netContainsNet(network, target) {
			return true
		}
	}
	return false
}

func matchCIDRDeny(target *net.IPNet, entries scopeEntries) bool {
	for _, ip := range entries.ips {
		if target.Contains(ip) {
			return true
		}
	}
	for _, network := range entries.nets {
		if netsOverlap(network, target) {
			return true
		}
	}
	return false
}

func netContainsNet(container, target *net.IPNet) bool {
	if !container.Contains(target.IP) {
		return false
	}
	containerOnes, _ := container.Mask.Size()
	targetOnes, _ := target.Mask.Size()
	return containerOnes <= targetOnes
}

func netsOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}
