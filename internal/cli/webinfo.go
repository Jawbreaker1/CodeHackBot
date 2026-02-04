package cli

import (
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strings"
)

func (r *Runner) handleWebInfo(goal, rawURL string) error {
	normalized, err := normalizeURL(rawURL)
	if err != nil {
		return err
	}
	host := hostFromURL(normalized)

	if err := r.handleBrowse([]string{normalized}); err != nil {
		return err
	}
	if host != "" {
		if hasCommand("whois") {
			if err := r.handleRun([]string{"whois", host}); err != nil {
				return err
			}
		}
		if hasCommand("dig") {
			if err := r.handleRun([]string{"dig", "+short", host}); err != nil {
				return err
			}
		} else if hasCommand("nslookup") {
			if err := r.handleRun([]string{"nslookup", host}); err != nil {
				return err
			}
		}
	}
	if hasCommand("curl") {
		if err := r.handleRun([]string{"curl", "-I", "-L", normalized}); err != nil {
			return err
		}
	}

	summaryPrompt := fmt.Sprintf("Summarize what %s is about and any hosting/ownership hints from the recent logs.", normalized)
	if host != "" {
		summaryPrompt = fmt.Sprintf("Summarize what %s (%s) is about and any hosting/ownership hints from the recent logs.", normalized, host)
	}
	return r.handleAsk(summaryPrompt)
}

func hostFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := parsed.Host
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		if splitHost, _, err := net.SplitHostPort(host); err == nil {
			host = splitHost
		}
	}
	return strings.TrimPrefix(host, "www.")
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
