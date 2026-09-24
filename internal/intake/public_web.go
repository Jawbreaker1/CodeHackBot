package intake

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxPageExcerpt = 24 << 10

type dnsObservation struct {
	Host          string    `json:"host"`
	Addresses     []string  `json:"addresses"`
	CanonicalName string    `json:"canonical_name,omitempty"`
	ObservedAt    time.Time `json:"observed_at"`
}

type pageObservation struct {
	URL         string            `json:"url"`
	Address     string            `json:"address"`
	Status      int               `json:"status"`
	Headers     map[string]string `json:"headers"`
	BodyExcerpt string            `json:"body_excerpt,omitempty"`
	Truncated   bool              `json:"truncated,omitempty"`
	ObservedAt  time.Time         `json:"observed_at"`
}

func publicDomain(raw string) (string, error) {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if len(host) < 4 || len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return "", fmt.Errorf("provide a public DNS hostname")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid DNS hostname")
		}
		for _, ch := range label {
			if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
				return "", fmt.Errorf("invalid DNS hostname")
			}
		}
	}
	for _, suffix := range []string{".local", ".localhost", ".internal", ".lan", ".home", ".test"} {
		if strings.HasSuffix(host, suffix) {
			return "", fmt.Errorf("local DNS names are unavailable to the public observation tool")
		}
	}
	return host, nil
}

func publicPageURL(raw string) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, fmt.Errorf("page URL is too long")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("provide an HTTP or HTTPS page URL without credentials, query, or fragment")
	}
	host, err := publicDomain(u.Hostname())
	if err != nil {
		return nil, err
	}
	if port := u.Port(); port != "" && !((u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443")) {
		return nil, fmt.Errorf("public page fetch uses only the standard HTTP or HTTPS port")
	}
	u.Host = host
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

func lookupPublicDNS(ctx context.Context, host string) (dnsObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return dnsObservation{}, fmt.Errorf("DNS lookup for %s: %w", host, err)
	}
	result := dnsObservation{Host: host, Addresses: make([]string, 0, len(addresses)), ObservedAt: time.Now().UTC()}
	for _, address := range addresses {
		result.Addresses = append(result.Addresses, address.IP.String())
	}
	sort.Strings(result.Addresses)
	if cname, err := net.DefaultResolver.LookupCNAME(ctx, host); err == nil {
		result.CanonicalName = strings.TrimSuffix(cname, ".")
	}
	return result, nil
}

func fetchPublicPage(ctx context.Context, raw string) (pageObservation, error) {
	u, err := publicPageURL(raw)
	if err != nil {
		return pageObservation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return pageObservation{}, fmt.Errorf("resolve page host: %w", err)
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, item := range resolved {
		if address, ok := netip.AddrFromSlice(item.IP); ok && publicAddress(address.Unmap()) {
			addresses = append(addresses, address.Unmap())
		}
	}
	if len(addresses) == 0 {
		return pageObservation{}, fmt.Errorf("page host has no public address")
	}
	port := "443"
	if u.Scheme == "http" {
		port = "80"
	}
	var connected string
	var connectedMu sync.Mutex
	transport := &http.Transport{
		Proxy:               nil,
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: 5 * time.Second,
		DialContext: func(dialCtx context.Context, network, endpoint string) (net.Conn, error) {
			requestedHost, requestedPort, splitErr := net.SplitHostPort(endpoint)
			if splitErr != nil || !strings.EqualFold(requestedHost, u.Hostname()) || requestedPort != port {
				return nil, fmt.Errorf("unexpected page connection target")
			}
			var lastErr error
			for _, address := range addresses {
				conn, dialErr := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(dialCtx, "tcp", net.JoinHostPort(address.String(), port))
				if dialErr == nil {
					connectedMu.Lock()
					connected = address.String()
					connectedMu.Unlock()
					return conn, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return pageObservation{}, err
	}
	request.Header.Set("User-Agent", "BirdHackBot/1.0 (read-only public observation)")
	response, err := client.Do(request)
	if err != nil {
		return pageObservation{}, fmt.Errorf("fetch public page: %w", err)
	}
	defer response.Body.Close()
	connectedMu.Lock()
	result := pageObservation{URL: u.String(), Address: connected, Status: response.StatusCode, Headers: boundedHeaders(response.Header), ObservedAt: time.Now().UTC()}
	connectedMu.Unlock()
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "json") || strings.Contains(contentType, "xml") {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxPageExcerpt+1))
		if readErr != nil {
			return result, fmt.Errorf("read public page: %w", readErr)
		}
		result.Truncated = len(body) > maxPageExcerpt
		if result.Truncated {
			body = body[:maxPageExcerpt]
		}
		result.BodyExcerpt = string(body)
	}
	return result, nil
}

func publicAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, excluded := range []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("198.18.0.0/15"),
	} {
		if excluded.Contains(address) {
			return false
		}
	}
	return true
}

func boundedHeaders(headers http.Header) map[string]string {
	// Keep useful page metadata, never response cookies or authentication values.
	keys := []string{"Allow", "Cache-Control", "Content-Length", "Content-Security-Policy", "Content-Type", "Date", "ETag", "Last-Modified", "Location", "Referrer-Policy", "Server", "Strict-Transport-Security", "X-Content-Type-Options", "X-Frame-Options"}
	result := make(map[string]string)
	for _, key := range keys {
		value := strings.Join(headers.Values(key), ", ")
		if value == "" {
			continue
		}
		if len(value) > 512 {
			value = value[:512]
		}
		result[key] = value
	}
	return result
}
