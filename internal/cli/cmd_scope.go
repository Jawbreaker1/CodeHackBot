package cli

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func (r *Runner) handleScope(args []string) error {
	if len(args) == 0 || strings.EqualFold(strings.TrimSpace(args[0]), "show") {
		r.printScopeStatus()
		return nil
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "add-target":
		if len(args) < 2 {
			return fmt.Errorf("usage: /scope add-target <host|ip|cidr|url>")
		}
		return r.addScopeTarget(args[1])
	case "remove-target", "rm-target":
		if len(args) < 2 {
			return fmt.Errorf("usage: /scope remove-target <host|ip|cidr|url>")
		}
		return r.removeScopeTarget(args[1])
	default:
		return fmt.Errorf("usage: /scope [show|add-target <host|ip|cidr|url>|remove-target <host|ip|cidr|url>]")
	}
}

func (r *Runner) printScopeStatus() {
	networks := append([]string{}, r.cfg.Scope.Networks...)
	targets := append([]string{}, r.cfg.Scope.Targets...)
	deny := append([]string{}, r.cfg.Scope.DenyTargets...)
	if len(networks) == 0 {
		networks = []string{"(none)"}
	}
	if len(targets) == 0 {
		targets = []string{"(none)"}
	}
	if len(deny) == 0 {
		deny = []string{"(none)"}
	}
	r.logger.Printf("Scope networks: %s", strings.Join(networks, ", "))
	r.logger.Printf("Scope targets: %s", strings.Join(targets, ", "))
	r.logger.Printf("Scope deny-targets: %s", strings.Join(deny, ", "))
	r.logger.Printf("Tip: use /scope add-target <host|ip|cidr|url> for authorized external targets.")
}

func (r *Runner) addScopeTarget(raw string) error {
	target, err := normalizeScopeTarget(raw)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("target is empty")
	}
	if isExternalScopeTarget(target) {
		ok, err := r.confirm(fmt.Sprintf("External target %q detected. Confirm written authorization and RoE exist, then add to session scope?", target))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("scope update not approved")
		}
	}
	before := len(r.cfg.Scope.Targets)
	r.cfg.Scope.Targets = appendUniqueFold(r.cfg.Scope.Targets, target)
	if len(r.cfg.Scope.Targets) == before {
		r.logger.Printf("Scope target already present: %s", target)
		return nil
	}
	if err := r.saveSessionConfig(); err != nil {
		return err
	}
	r.logger.Printf("Scope target added: %s", target)
	return nil
}

func (r *Runner) removeScopeTarget(raw string) error {
	target, err := normalizeScopeTarget(raw)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("target is empty")
	}
	next := make([]string, 0, len(r.cfg.Scope.Targets))
	removed := false
	for _, existing := range r.cfg.Scope.Targets {
		if strings.EqualFold(strings.TrimSpace(existing), target) {
			removed = true
			continue
		}
		next = append(next, existing)
	}
	if !removed {
		r.logger.Printf("Scope target not found: %s", target)
		return nil
	}
	r.cfg.Scope.Targets = next
	if err := r.saveSessionConfig(); err != nil {
		return err
	}
	r.logger.Printf("Scope target removed: %s", target)
	return nil
}

func (r *Runner) saveSessionConfig() error {
	if _, err := r.ensureSessionScaffold(); err != nil {
		return err
	}
	sessionConfigPath := config.SessionPath(r.cfg.Session.LogDir, r.sessionID)
	if err := config.Save(sessionConfigPath, r.cfg); err != nil {
		return fmt.Errorf("save session config: %w", err)
	}
	r.logger.Printf("Session config saved at %s", filepath.Clean(sessionConfigPath))
	return nil
}

func appendUniqueFold(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return values
		}
	}
	return append(values, value)
}

func normalizeScopeTarget(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("target is empty")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("invalid target url: %w", err)
		}
		host := strings.TrimSpace(parsed.Hostname())
		if host == "" {
			return "", fmt.Errorf("invalid target url host")
		}
		return strings.ToLower(host), nil
	}
	if strings.HasPrefix(value, "www.") {
		return strings.ToLower(value), nil
	}
	if parsed, err := url.Parse("https://" + value); err == nil && strings.TrimSpace(parsed.Hostname()) != "" {
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if host != "" && !strings.Contains(value, "/") {
			return host, nil
		}
	}
	return strings.ToLower(value), nil
}

func isExternalScopeTarget(target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	if target == "localhost" {
		return false
	}
	if ip := net.ParseIP(target); ip != nil {
		return isExternalIP(ip)
	}
	if _, network, err := net.ParseCIDR(target); err == nil && network != nil {
		return isExternalIP(network.IP)
	}
	return true
}

func isExternalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return true
}
