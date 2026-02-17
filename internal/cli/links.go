package cli

import (
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	parseLinksMaxBytes = 2_000_000
	parseLinksMaxOut   = 250
)

var (
	baseHrefPattern   = regexp.MustCompile(`(?is)<base\b[^>]*\bhref\s*=\s*("([^"]+)"|'([^']+)'|([^'"\s>]+))`)
	aHrefPattern      = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*("([^"]+)"|'([^']+)'|([^'"\s>]+))`)
	linkHrefPattern   = regexp.MustCompile(`(?is)<link\b[^>]*\bhref\s*=\s*("([^"]+)"|'([^']+)'|([^'"\s>]+))`)
	scriptSrcPattern  = regexp.MustCompile(`(?is)<script\b[^>]*\bsrc\s*=\s*("([^"]+)"|'([^']+)'|([^'"\s>]+))`)
	imgSrcPattern     = regexp.MustCompile(`(?is)<img\b[^>]*\bsrc\s*=\s*("([^"]+)"|'([^']+)'|([^'"\s>]+))`)
	formActionPattern = regexp.MustCompile(`(?is)<form\b[^>]*\baction\s*=\s*("([^"]+)"|'([^']+)'|([^'"\s>]+))`)
	iframeSrcPattern  = regexp.MustCompile(`(?is)<iframe\b[^>]*\bsrc\s*=\s*("([^"]+)"|'([^']+)'|([^'"\s>]+))`)
)

func (r *Runner) handleParseLinks(args []string) error {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}

	var inputPath string
	baseURL := ""
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(arg), "base=") {
			baseURL = strings.TrimSpace(strings.SplitN(arg, "=", 2)[1])
			continue
		}
		if inputPath == "" {
			inputPath = arg
			continue
		}
	}
	if inputPath == "" {
		inputPath = strings.TrimSpace(r.lastBrowseBodyPath)
	}
	if inputPath == "" {
		return fmt.Errorf("parse_links: no html path provided and no recent browse body saved; run /browse first or pass a file path")
	}

	// If we weren't given a base, try to reuse the last browsed URL.
	if baseURL == "" {
		baseURL = strings.TrimSpace(r.lastBrowseURL)
	}
	baseParsed, _ := normalizeURLForResolve(baseURL)

	r.setTask("parse_links")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("parse_links read: %w", err)
	}
	if len(data) > parseLinksMaxBytes {
		data = data[:parseLinksMaxBytes]
	}

	links, err := extractLinksHTML(data, baseParsed)
	if err != nil {
		return err
	}
	if len(links) > parseLinksMaxOut {
		links = links[:parseLinksMaxOut]
	}

	outPath, logPath, err := writeLinksArtifacts(sessionDir, inputPath, baseURL, links)
	if err != nil {
		return err
	}

	safePrintf("Links found: %d\n", len(links))
	safePrintf("Links saved: %s\n", outPath)
	safePrintf("Log saved: %s\n", logPath)
	if len(links) > 0 {
		safePrintf("Top links:\n")
		for i, link := range links {
			if i >= 20 {
				break
			}
			safePrintf("- %s\n", link)
		}
	}

	r.recordActionArtifact(logPath)
	obsArgs := []string{inputPath}
	if strings.TrimSpace(baseURL) != "" {
		obsArgs = append(obsArgs, "base="+baseURL)
	}
	r.recordObservation("parse_links", obsArgs, logPath, fmt.Sprintf("links=%d out=%s", len(links), outPath), nil)
	r.maybeAutoSummarize(logPath, "parse_links")
	return nil
}

func normalizeURLForResolve(raw string) (*neturl.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty url")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid base url: %s", raw)
	}
	return parsed, nil
}

func extractLinksHTML(data []byte, base *neturl.URL) ([]string, error) {
	// We avoid a full HTML parser to keep dependencies offline-friendly.
	content := string(data)

	// Prefer <base href="..."> if present and valid.
	if baseHref := firstAttrMatch(baseHrefPattern, content); baseHref != "" {
		if parsed, err := neturl.Parse(strings.TrimSpace(baseHref)); err == nil {
			if parsed.IsAbs() {
				base = parsed
			} else if base != nil {
				base = base.ResolveReference(parsed)
			}
		}
	}

	seen := map[string]struct{}{}
	out := []string{}

	for _, pattern := range []*regexp.Regexp{
		aHrefPattern,
		linkHrefPattern,
		scriptSrcPattern,
		imgSrcPattern,
		formActionPattern,
		iframeSrcPattern,
	} {
		for _, raw := range allAttrMatches(pattern, content) {
			if isNonNavigationalScheme(raw) {
				continue
			}
			normalized := normalizeLink(raw, base)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}

	sort.Strings(out)
	return out, nil
}

func firstAttrMatch(pattern *regexp.Regexp, content string) string {
	match := pattern.FindStringSubmatch(content)
	if len(match) < 5 {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(match[2], match[3], match[4]))
}

func allAttrMatches(pattern *regexp.Regexp, content string) []string {
	matches := pattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 5 {
			continue
		}
		value := strings.TrimSpace(firstNonEmpty(match[2], match[3], match[4]))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func isNonNavigationalScheme(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "javascript:") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "tel:") ||
		strings.HasPrefix(lower, "data:")
}

func normalizeLink(raw string, base *neturl.URL) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Protocol-relative URL.
	if strings.HasPrefix(raw, "//") {
		if base != nil && base.Scheme != "" {
			raw = base.Scheme + ":" + raw
		} else {
			raw = "https:" + raw
		}
	}

	u, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	if !u.IsAbs() {
		if base == nil {
			// Without base, keep relative paths (useful for wordlists/crawl).
			if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
				return stripFragment(raw)
			}
			return ""
		}
		u = base.ResolveReference(u)
	}
	u.Fragment = ""
	return strings.TrimSpace(u.String())
}

func stripFragment(raw string) string {
	if idx := strings.Index(raw, "#"); idx >= 0 {
		return raw[:idx]
	}
	return raw
}

func writeLinksArtifacts(sessionDir, inputPath, baseURL string, links []string) (string, string, error) {
	webDir := filepath.Join(sessionDir, "artifacts", "web")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		return "", "", err
	}
	logDir := filepath.Join(sessionDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", "", err
	}
	timestamp := time.Now().UTC().Format("20060102-150405.000000000")
	outPath := filepath.Join(webDir, fmt.Sprintf("links-%s.txt", timestamp))
	logPath := filepath.Join(logDir, fmt.Sprintf("links-%s.log", timestamp))

	builder := strings.Builder{}
	for _, link := range links {
		builder.WriteString(link)
		builder.WriteString("\n")
	}
	if err := os.WriteFile(outPath, []byte(builder.String()), 0o644); err != nil {
		return "", "", err
	}

	log := strings.Builder{}
	log.WriteString(fmt.Sprintf("Input: %s\n", inputPath))
	if strings.TrimSpace(baseURL) != "" {
		log.WriteString(fmt.Sprintf("Base: %s\n", strings.TrimSpace(baseURL)))
	}
	log.WriteString(fmt.Sprintf("Links: %d\n", len(links)))
	log.WriteString(fmt.Sprintf("Output: %s\n", outPath))
	if len(links) > 0 {
		log.WriteString("\nTop links:\n")
		for i, link := range links {
			if i >= 50 {
				break
			}
			log.WriteString("- " + link + "\n")
		}
	}
	if err := os.WriteFile(logPath, []byte(log.String()), 0o644); err != nil {
		return "", "", err
	}

	return outPath, logPath, nil
}
