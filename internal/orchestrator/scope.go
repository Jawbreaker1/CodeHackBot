package orchestrator

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var privateCIDRAliases = map[string][]string{
	"internal": {"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16"},
	"private":  {"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
	"loopback": {"127.0.0.0/8"},
	"local":    {"127.0.0.0/8"},
}

var (
	scopeIPv4CIDRPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}\b`)
	scopeIPv4Pattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	scopeHostPattern     = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\b`)
)

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

func (p *ScopePolicy) ValidateCommandTargets(command string, args []string) error {
	targets := p.extractTargets(command, args)
	if len(targets) == 0 {
		if isNetworkSensitiveCommand(command) {
			return fmt.Errorf("scope violation: could not infer target from network command %q", strings.TrimSpace(command))
		}
		return nil
	}
	task := TaskSpec{Targets: targets}
	return p.ValidateTaskTargets(task)
}

func (p *ScopePolicy) FirstAllowedTarget() string {
	if p == nil {
		return ""
	}
	for _, network := range p.allowNets {
		if network == nil {
			continue
		}
		value := strings.TrimSpace(network.String())
		if value != "" {
			return value
		}
	}
	for _, ip := range p.allowIPs {
		if ip == nil {
			continue
		}
		value := strings.TrimSpace(ip.String())
		if value != "" {
			return value
		}
	}
	for _, literal := range p.allowLiterals {
		value := strings.TrimSpace(literal)
		if value != "" {
			return value
		}
	}
	return ""
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

func (p *ScopePolicy) extractTargets(command string, args []string) []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "" {
			return
		}
		seen[v] = struct{}{}
	}
	candidates := append([]string{command}, args...)
	for _, token := range candidates {
		lower := strings.ToLower(token)
		if host := hostFromURL(token); host != "" {
			add(host)
		}
		if strings.Contains(lower, "localhost") {
			add("localhost")
		}
		for _, match := range scopeIPv4CIDRPattern.FindAllString(token, -1) {
			add(match)
		}
		for _, match := range scopeIPv4Pattern.FindAllString(token, -1) {
			add(match)
		}
		for _, match := range scopeHostPattern.FindAllString(token, -1) {
			add(match)
		}
		for _, literal := range p.allowLiterals {
			if literal != "" && strings.Contains(lower, literal) {
				add(literal)
			}
		}
		for _, literal := range p.denyLiterals {
			if literal != "" && strings.Contains(lower, literal) {
				add(literal)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for target := range seen {
		out = append(out, target)
	}
	return out
}

func hostFromURL(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" || !strings.Contains(trimmed, "://") {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host
}

func isNetworkSensitiveCommand(command string) bool {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd == "" {
		return false
	}
	if strings.Contains(cmd, "/") {
		parts := strings.Split(cmd, "/")
		cmd = parts[len(parts)-1]
	}
	switch cmd {
	case "browse", "crawl",
		"curl", "wget",
		"nmap", "nc", "netcat",
		"dig", "nslookup", "host", "whois",
		"ping", "traceroute", "tracepath",
		"ssh", "scp", "sftp", "ftp", "telnet",
		"httpx", "nikto", "ffuf":
		return true
	default:
		return false
	}
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
