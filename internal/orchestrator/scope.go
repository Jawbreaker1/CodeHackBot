package orchestrator

import (
	"fmt"
	"net"
	"strings"
)

var privateCIDRAliases = map[string][]string{
	"internal": {"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16"},
	"private":  {"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
	"loopback": {"127.0.0.0/8"},
	"local":    {"127.0.0.0/8"},
}

type ScopePolicy struct {
	allowIPs      []net.IP
	allowNets     []*net.IPNet
	allowLiterals []string
	denyIPs       []net.IP
	denyNets      []*net.IPNet
	denyLiterals  []string
}

func NewScopePolicy(scope Scope) *ScopePolicy {
	p := &ScopePolicy{}
	p.loadAllow(append(scope.Networks, scope.Targets...))
	p.loadDeny(scope.DenyTargets)
	return p
}

func (p *ScopePolicy) ValidateTaskTargets(task TaskSpec) error {
	if len(task.Targets) == 0 {
		return nil
	}
	allowEnforced := len(p.allowIPs) > 0 || len(p.allowNets) > 0 || len(p.allowLiterals) > 0
	violations := make([]string, 0)
	for _, raw := range task.Targets {
		target := strings.ToLower(strings.TrimSpace(raw))
		if target == "" {
			continue
		}
		ip, cidr, literal := parseScopeTarget(target)
		if literal {
			if containsLower(p.denyLiterals, target) {
				violations = append(violations, fmt.Sprintf("denied target %s", raw))
				continue
			}
			if allowEnforced && !containsLower(p.allowLiterals, target) {
				violations = append(violations, fmt.Sprintf("out of scope target %s", raw))
			}
			continue
		}
		if ip != nil {
			if matchIP(ip, p.denyIPs, p.denyNets) {
				violations = append(violations, fmt.Sprintf("denied target %s", raw))
				continue
			}
			if allowEnforced && !matchIP(ip, p.allowIPs, p.allowNets) {
				violations = append(violations, fmt.Sprintf("out of scope target %s", raw))
			}
			continue
		}
		if cidr != nil {
			if matchNetDeny(cidr, p.denyIPs, p.denyNets) {
				violations = append(violations, fmt.Sprintf("denied target %s", raw))
				continue
			}
			if allowEnforced && !matchNetAllow(cidr, p.allowNets) {
				violations = append(violations, fmt.Sprintf("out of scope target %s", raw))
			}
		}
	}
	if len(violations) > 0 {
		return fmt.Errorf("scope violation: %s", strings.Join(violations, "; "))
	}
	return nil
}

func (p *ScopePolicy) loadAllow(entries []string) {
	p.allowIPs, p.allowNets, p.allowLiterals = parseScopeEntries(entries)
}

func (p *ScopePolicy) loadDeny(entries []string) {
	p.denyIPs, p.denyNets, p.denyLiterals = parseScopeEntries(entries)
}

func parseScopeEntries(entries []string) ([]net.IP, []*net.IPNet, []string) {
	ips := []net.IP{}
	nets := []*net.IPNet{}
	literals := []string{}
	for _, entry := range expandScopeAliases(entries) {
		token := strings.ToLower(strings.TrimSpace(entry))
		if token == "" {
			continue
		}
		if strings.Contains(token, "/") {
			if _, network, err := net.ParseCIDR(token); err == nil {
				nets = append(nets, network)
				continue
			}
		}
		if ip := net.ParseIP(token); ip != nil {
			ips = append(ips, ip)
			continue
		}
		literals = append(literals, token)
	}
	return ips, nets, literals
}

func expandScopeAliases(entries []string) []string {
	out := []string{}
	for _, entry := range entries {
		token := strings.ToLower(strings.TrimSpace(entry))
		if alias, ok := privateCIDRAliases[token]; ok {
			out = append(out, alias...)
			continue
		}
		out = append(out, entry)
	}
	return out
}

func parseScopeTarget(target string) (net.IP, *net.IPNet, bool) {
	if target == "localhost" {
		return net.ParseIP("127.0.0.1"), nil, false
	}
	if strings.Contains(target, "/") {
		if _, network, err := net.ParseCIDR(target); err == nil {
			return nil, network, false
		}
	}
	if ip := net.ParseIP(target); ip != nil {
		return ip, nil, false
	}
	return nil, nil, true
}

func containsLower(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func matchIP(ip net.IP, ips []net.IP, nets []*net.IPNet) bool {
	for _, allowed := range ips {
		if allowed.Equal(ip) {
			return true
		}
	}
	for _, network := range nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func matchNetAllow(target *net.IPNet, allow []*net.IPNet) bool {
	for _, network := range allow {
		if netContains(network, target) {
			return true
		}
	}
	return false
}

func matchNetDeny(target *net.IPNet, denyIPs []net.IP, denyNets []*net.IPNet) bool {
	for _, ip := range denyIPs {
		if target.Contains(ip) {
			return true
		}
	}
	for _, network := range denyNets {
		if netsOverlap(network, target) {
			return true
		}
	}
	return false
}

func netContains(container, target *net.IPNet) bool {
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
