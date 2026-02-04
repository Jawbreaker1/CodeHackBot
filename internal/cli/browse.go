package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const browseBodyLimit = 1_000_000

func (r *Runner) handleBrowse(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /browse <url>")
	}
	raw := strings.TrimSpace(args[0])
	if raw == "" {
		return fmt.Errorf("usage: /browse <url>")
	}
	target, err := normalizeURL(raw)
	if err != nil {
		return err
	}

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}

	if r.cfg.Network.AssumeOffline {
		ok, err := r.confirm(fmt.Sprintf("Network is disabled by config. Allow this fetch? %s", target))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("network access not approved")
		}
	} else {
		ok, err := r.confirm(fmt.Sprintf("Fetch URL: %s?", target))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("fetch not approved")
		}
	}

	r.setTask("browse")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("User-Agent", "BirdHackBot/0.1 (+lab)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if altURL, ok := alternateHostURL(target); ok {
			reqAlt, altErr := http.NewRequestWithContext(ctx, http.MethodGet, altURL, nil)
			if altErr == nil {
				reqAlt.Header.Set("User-Agent", "BirdHackBot/0.1 (+lab)")
				respAlt, altDoErr := http.DefaultClient.Do(reqAlt)
				if altDoErr == nil {
					resp = respAlt
					target = altURL
				} else if isDNSError(err) || strings.Contains(err.Error(), "no such host") {
					return fmt.Errorf("fetch: %w (also tried %s)", err, altURL)
				}
			}
		} else if isDNSError(err) || strings.Contains(err.Error(), "no such host") {
			return fmt.Errorf("fetch: %w (check spelling or DNS)", err)
		}
		if resp == nil {
			return fmt.Errorf("fetch: %w", err)
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, browseBodyLimit))
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	content := string(body)
	title := extractTitle(content)
	text := collapseWhitespace(stripHTML(content))
	snippet := truncate(text, 2000)

	fmt.Printf("URL: %s\n", target)
	if title != "" {
		fmt.Printf("Title: %s\n", title)
	}
	fmt.Printf("Status: %d\n", resp.StatusCode)
	if snippet != "" {
		fmt.Printf("Snippet:\n%s\n", snippet)
	}

	logPath, err := writeBrowseLog(sessionDir, target, resp.StatusCode, resp.Header.Get("Content-Type"), title, snippet)
	if err == nil {
		r.maybeAutoSummarize(logPath, "browse")
	}
	return nil
}

func normalizeURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("url is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("invalid url host")
	}
	return parsed.String(), nil
}

func alternateHostURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	host := parsed.Host
	if strings.HasPrefix(host, "www.") {
		parsed.Host = strings.TrimPrefix(host, "www.")
	} else {
		parsed.Host = "www." + host
	}
	return parsed.String(), true
}

func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	if err == nil {
		return false
	}
	if errors.As(err, &dnsErr) {
		return true
	}
	return false
}

func extractTitle(content string) string {
	lower := strings.ToLower(content)
	start := strings.Index(lower, "<title")
	if start == -1 {
		return ""
	}
	start = strings.Index(lower[start:], ">")
	if start == -1 {
		return ""
	}
	start += strings.Index(lower, "<title") + 1
	end := strings.Index(lower[start:], "</title>")
	if end == -1 {
		return ""
	}
	title := content[start : start+end]
	title = stripHTML(title)
	title = collapseWhitespace(title)
	return strings.TrimSpace(title)
}

func stripHTML(content string) string {
	var builder strings.Builder
	inTag := false
	for _, r := range content {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				builder.WriteRune(r)
			}
		}
	}
	return builder.String()
}

func collapseWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func truncate(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func writeBrowseLog(sessionDir, url string, status int, contentType, title, snippet string) (string, error) {
	logDir := filepath.Join(sessionDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	timestamp := time.Now().UTC().Format("20060102-150405.000000000")
	path := filepath.Join(logDir, fmt.Sprintf("web-%s.log", timestamp))
	builder := strings.Builder{}
	builder.WriteString(fmt.Sprintf("URL: %s\nStatus: %d\n", url, status))
	if contentType != "" {
		builder.WriteString(fmt.Sprintf("Content-Type: %s\n", contentType))
	}
	if title != "" {
		builder.WriteString(fmt.Sprintf("Title: %s\n", title))
	}
	if snippet != "" {
		builder.WriteString("\nSnippet:\n")
		builder.WriteString(snippet)
		builder.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
