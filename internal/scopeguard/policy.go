package scopeguard

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var privateCIDRAliases = map[string][]string{
	"internal":  {"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16"},
	"private":   {"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
	"loopback":  {"127.0.0.0/8"},
	"local":     {"127.0.0.0/8"},
	"linklocal": {"169.254.0.0/16"},
}

var (
	ipv4CIDRPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}\b`)
	ipv4Pattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	hostPattern     = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}\b`)
)

var fileLikeHostExtensions = map[string]struct{}{
	".cfg": {}, ".conf": {}, ".csv": {}, ".gnmap": {}, ".ini": {}, ".json": {}, ".log": {}, ".md": {}, ".nmap": {}, ".out": {}, ".py": {}, ".rb": {}, ".sh": {}, ".txt": {}, ".xml": {}, ".yaml": {}, ".yml": {}, ".zip": {},
}

var networkCommandSet = map[string]struct{}{
	"browse": {}, "crawl": {},
	"curl": {}, "wget": {},
	"nmap": {}, "masscan": {}, "naabu": {},
	"nc": {}, "netcat": {},
	"dig": {}, "nslookup": {}, "host": {}, "whois": {},
	"ping": {}, "traceroute": {}, "tracepath": {},
	"ssh": {}, "scp": {}, "sftp": {}, "ftp": {}, "telnet": {},
	"httpx": {}, "nikto": {}, "ffuf": {}, "amass": {}, "subfinder": {},
	"msfconsole": {}, "searchsploit": {},
}

var shellWrapperSet = map[string]struct{}{
	"bash": {}, "sh": {}, "zsh": {}, "ash": {}, "dash": {}, "ksh": {},
}

type Policy struct {
	allowIPs      []net.IP
	allowNets     []*net.IPNet
	allowLiterals []string
	denyIPs       []net.IP
	denyNets      []*net.IPNet
	denyLiterals  []string
}

type ValidateOptions struct {
	FailClosedNetwork  bool
	FailClosedWrappers bool
}

func New(allowEntries, denyEntries []string) *Policy {
	allowIPs, allowNets, allowLiterals := parseScopeEntries(allowEntries)
	denyIPs, denyNets, denyLiterals := parseScopeEntries(denyEntries)
	return &Policy{
		allowIPs:      allowIPs,
		allowNets:     allowNets,
		allowLiterals: allowLiterals,
		denyIPs:       denyIPs,
		denyNets:      denyNets,
		denyLiterals:  denyLiterals,
	}
}

func (p *Policy) HasRules() bool {
	if p == nil {
		return false
	}
	return len(p.allowIPs) > 0 || len(p.allowNets) > 0 || len(p.allowLiterals) > 0 ||
		len(p.denyIPs) > 0 || len(p.denyNets) > 0 || len(p.denyLiterals) > 0
}

func (p *Policy) FirstAllowedTarget() string {
	if p == nil {
		return ""
	}
	for _, network := range p.allowNets {
		if network == nil {
			continue
		}
		if value := strings.TrimSpace(network.String()); value != "" {
			return value
		}
	}
	for _, ip := range p.allowIPs {
		if ip == nil {
			continue
		}
		if value := strings.TrimSpace(ip.String()); value != "" {
			return value
		}
	}
	for _, literal := range p.allowLiterals {
		if value := strings.TrimSpace(literal); value != "" {
			return value
		}
	}
	return ""
}

func (p *Policy) ValidateTargets(rawTargets []string) error {
	if p == nil || len(rawTargets) == 0 {
		return nil
	}
	allowEnforced := len(p.allowIPs) > 0 || len(p.allowNets) > 0 || len(p.allowLiterals) > 0
	violations := make([]string, 0)
	for _, raw := range rawTargets {
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

func (p *Policy) ValidateCommand(command string, args []string, opts ValidateOptions) error {
	if p == nil || !p.HasRules() {
		return nil
	}
	targets, networkSensitive, wrapperNetwork := p.extractTargetsAndSignals(command, args)
	if len(targets) == 0 {
		if opts.FailClosedNetwork && networkSensitive {
			return fmt.Errorf("scope violation: could not infer target from network command %q", strings.TrimSpace(command))
		}
		if opts.FailClosedWrappers && wrapperNetwork {
			return fmt.Errorf("scope violation: could not infer target from wrapped network command %q", strings.TrimSpace(command))
		}
		return nil
	}
	return p.ValidateTargets(targets)
}

func (p *Policy) ExtractTargets(command string, args []string) []string {
	if p == nil {
		return nil
	}
	targets, _, _ := p.extractTargetsAndSignals(command, args)
	return targets
}

func (p *Policy) extractTargetsAndSignals(command string, args []string) ([]string, bool, bool) {
	seen := map[string]struct{}{}
	add := func(value string) {
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "" {
			return
		}
		seen[v] = struct{}{}
	}
	processToken := func(token string) {
		isFileLike := looksLikeFileToken(token)
		if host := hostFromURL(token); host != "" {
			add(host)
		}
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
		if !isFileLike {
			for _, match := range hostPattern.FindAllString(token, -1) {
				add(match)
			}
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

	networkSensitive := isNetworkSensitiveCommand(command)
	wrapperNetwork := false
	processToken(command)
	for _, arg := range args {
		processToken(arg)
	}

	if script, ok := wrapperScript(command, args); ok {
		wrapperNetwork = scriptContainsNetworkIntent(script)
		if wrapperNetwork {
			networkSensitive = true
		}
		processToken(script)
		for _, field := range strings.Fields(script) {
			processToken(field)
			if isNetworkSensitiveCommand(field) {
				networkSensitive = true
				wrapperNetwork = true
			}
		}
	}

	out := make([]string, 0, len(seen))
	for target := range seen {
		out = append(out, target)
	}
	sort.Strings(out)
	return out, networkSensitive, wrapperNetwork
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
	out := make([]string, 0, len(entries))
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

func commandName(command string) string {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd == "" {
		return ""
	}
	return filepath.Base(cmd)
}

func isNetworkSensitiveCommand(command string) bool {
	cmd := commandName(command)
	if cmd == "" {
		return false
	}
	_, ok := networkCommandSet[cmd]
	return ok
}

func IsNetworkSensitiveCommand(command string) bool {
	return isNetworkSensitiveCommand(command)
}

func isShellWrapper(command string) bool {
	cmd := commandName(command)
	if cmd == "" {
		return false
	}
	_, ok := shellWrapperSet[cmd]
	return ok
}

func wrapperScript(command string, args []string) (string, bool) {
	if !isShellWrapper(command) || len(args) == 0 {
		return "", false
	}
	for i, arg := range args {
		token := strings.TrimSpace(arg)
		if token == "" || !strings.HasPrefix(token, "-") {
			continue
		}
		if token == "-c" {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if strings.Contains(token, "c") {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
	}
	return "", false
}

func scriptContainsNetworkIntent(script string) bool {
	text := strings.ToLower(strings.TrimSpace(script))
	if text == "" {
		return false
	}
	if strings.Contains(text, "://") {
		return true
	}
	if ipv4CIDRPattern.MatchString(text) || ipv4Pattern.MatchString(text) || hostPattern.MatchString(text) {
		return true
	}
	for cmd := range networkCommandSet {
		if hasToken(text, cmd) {
			return true
		}
	}
	return false
}

func hasToken(text, token string) bool {
	if text == token {
		return true
	}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '/' || r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if part == token {
			return true
		}
	}
	return false
}

func looksLikeFileToken(token string) bool {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, "~") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(trimmed))
	if ext == "" {
		return false
	}
	_, ok := fileLikeHostExtensions[ext]
	return ok
}
