package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

const (
	crawlDefaultMaxPages = 10
	crawlDefaultMaxDepth = 2
	crawlBodyLimit       = browseBodyLimit
)

type crawlIndex struct {
	CrawlID    string      `json:"crawl_id"`
	StartURL   string      `json:"start_url"`
	SameHost   bool        `json:"same_host"`
	MaxPages   int         `json:"max_pages"`
	MaxDepth   int         `json:"max_depth"`
	Pages      []crawlPage `json:"pages"`
	Discovered int         `json:"discovered_total"`
	Visited    int         `json:"visited_total"`
	Errors     []string    `json:"errors,omitempty"`
}

type crawlPage struct {
	URL           string   `json:"url"`
	Depth         int      `json:"depth"`
	Status        int      `json:"status"`
	ContentType   string   `json:"content_type,omitempty"`
	Title         string   `json:"title,omitempty"`
	MetaDesc      string   `json:"meta_description,omitempty"`
	Headings      []string `json:"headings,omitempty"`
	Snippet       string   `json:"snippet,omitempty"`
	BodyPath      string   `json:"body_path,omitempty"`
	LinksPath     string   `json:"links_path,omitempty"`
	DiscoveredOut int      `json:"discovered_links,omitempty"`
}

func (r *Runner) handleCrawl(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		if strings.TrimSpace(r.lastBrowseURL) != "" {
			args = []string{r.lastBrowseURL}
		} else {
			return fmt.Errorf("usage: /crawl <start_url> [max_pages=N] [max_depth=N] [same_host=true|false]")
		}
	}

	startRaw := strings.TrimSpace(args[0])
	startURL, err := normalizeURL(startRaw)
	if err != nil {
		return err
	}
	maxPages, maxDepth, sameHost, err := parseCrawlOptions(args[1:])
	if err != nil {
		return err
	}
	if maxPages <= 0 {
		maxPages = crawlDefaultMaxPages
	}
	if maxDepth < 0 {
		maxDepth = crawlDefaultMaxDepth
	}

	parsedStart, err := neturl.Parse(startURL)
	if err != nil {
		return fmt.Errorf("invalid start url: %w", err)
	}
	startHost := strings.ToLower(strings.TrimSpace(parsedStart.Host))

	// One approval up front.
	if r.cfg.Network.AssumeOffline {
		ok, err := r.confirm(fmt.Sprintf("Network is disabled by config. Allow crawl (%d page(s))? %s", maxPages, startURL))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("network access not approved")
		}
	} else {
		ok, err := r.confirm(fmt.Sprintf("Crawl (%d page(s)): %s?", maxPages, startURL))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("fetch not approved")
		}
	}

	r.setTask("crawl")
	defer r.clearTask()
	stopIndicator := r.startWorkingIndicator(newActivityWriter(r.liveWriter()))
	defer stopIndicator()

	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	r.updateKnownTargetFromText(startURL)

	crawlID := time.Now().UTC().Format("20060102-150405.000000000")
	webRoot := filepath.Join(sessionDir, "artifacts", "web")
	pagesDir := filepath.Join(webRoot, "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		return fmt.Errorf("create pages dir: %w", err)
	}

	index := crawlIndex{
		CrawlID:  crawlID,
		StartURL: startURL,
		SameHost: sameHost,
		MaxPages: maxPages,
		MaxDepth: maxDepth,
	}

	type queued struct {
		url   string
		depth int
	}
	queue := []queued{{url: canonicalizeURL(startURL), depth: 0}}
	visited := map[string]struct{}{}
	discovered := 1

	client := &http.Client{Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(maxPages)*30*time.Second)
	defer cancel()

	for len(queue) > 0 && len(index.Pages) < maxPages {
		item := queue[0]
		queue = queue[1:]
		if _, ok := visited[item.url]; ok {
			continue
		}
		visited[item.url] = struct{}{}

		if item.depth > maxDepth {
			continue
		}

		page, outLinks, fetchErr := r.fetchAndIndexPage(ctx, client, sessionDir, pagesDir, item.url, item.depth)
		if fetchErr != nil {
			index.Errors = append(index.Errors, fmt.Sprintf("%s: %v", item.url, fetchErr))
		}
		index.Pages = append(index.Pages, page)

		// Enqueue discovered links.
		for _, link := range outLinks {
			link = canonicalizeURL(link)
			if link == "" {
				continue
			}
			if _, ok := visited[link]; ok {
				continue
			}
			if sameHost {
				parsed, err := neturl.Parse(link)
				if err != nil {
					continue
				}
				host := strings.ToLower(strings.TrimSpace(parsed.Host))
				if host == "" || host != startHost {
					continue
				}
			}
			// Depth-first would bias; BFS is fine.
			queue = append(queue, queued{url: link, depth: item.depth + 1})
			discovered++
		}
	}

	index.Discovered = discovered
	index.Visited = len(visited)

	// Write index JSON.
	indexPath := filepath.Join(webRoot, "crawl-index.json")
	indexArchivePath := filepath.Join(webRoot, fmt.Sprintf("crawl-index-%s.json", crawlID))
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal crawl index: %w", err)
	}
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		return fmt.Errorf("write crawl index: %w", err)
	}
	_ = os.WriteFile(indexArchivePath, data, 0o644)

	// Write crawl log.
	logText := strings.Builder{}
	logText.WriteString(fmt.Sprintf("CrawlID: %s\nStart: %s\nSameHost: %v\nMaxPages: %d\nMaxDepth: %d\nVisited: %d\nDiscovered: %d\nIndex: %s\n",
		crawlID, startURL, sameHost, maxPages, maxDepth, index.Visited, index.Discovered, indexPath))
	if len(index.Errors) > 0 {
		logText.WriteString("\nErrors:\n")
		for i, e := range index.Errors {
			if i >= 20 {
				logText.WriteString("...\n")
				break
			}
			logText.WriteString("- " + e + "\n")
		}
	}
	logPath, _ := writeFSLog(sessionDir, "crawl", logText.String())

	safePrintf("Crawl complete: %d page(s)\n", len(index.Pages))
	safePrintf("Index: %s\n", indexPath)
	if logPath != "" {
		safePrintf("Log saved: %s\n", logPath)
	}

	r.recordActionArtifact(logPath)
	r.recordObservation("crawl", []string{startURL, fmt.Sprintf("max_pages=%d", maxPages), fmt.Sprintf("max_depth=%d", maxDepth)}, logPath, fmt.Sprintf("pages=%d index=%s", len(index.Pages), indexPath), nil)
	r.maybeAutoSummarize(logPath, "crawl")

	// If LLM is available, produce an immediate crawl summary artifact so /assist can synthesize without re-fetching.
	_ = r.maybeSummarizeCrawl(indexPath, len(index.Pages))
	return nil
}

func (r *Runner) maybeSummarizeCrawl(indexPath string, pages int) error {
	if pages <= 0 || strings.TrimSpace(indexPath) == "" {
		return nil
	}
	if !r.llmAllowed() {
		return nil
	}
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	// Keep prompt bounded.
	const maxIndexBytes = 24_000
	if len(data) > maxIndexBytes {
		data = data[:maxIndexBytes]
	}

	artifacts, _ := memory.EnsureArtifacts(sessionDir)
	contextSummary := readFileTrimmed(artifacts.SummaryPath)
	contextFacts := readFileTrimmed(artifacts.FactsPath)
	contextFocus := readFileTrimmed(artifacts.FocusPath)

	prompt := strings.Builder{}
	prompt.WriteString("You are summarizing a bounded web crawl for an authorized security assessment.\n")
	prompt.WriteString("Task: produce a concise summary of what the site is about and a security-relevant overview.\n")
	prompt.WriteString("Include: tech stack hints, key sections/endpoints, any obvious issues (missing headers, exposed paths), unknowns, and 3 next concrete steps.\n\n")
	prompt.WriteString("Crawl index (JSON):\n")
	prompt.WriteString(string(data))
	prompt.WriteString("\n\n")
	if contextSummary != "" {
		prompt.WriteString("Session summary:\n" + contextSummary + "\n\n")
	}
	if contextFacts != "" {
		prompt.WriteString("Known facts:\n" + contextFacts + "\n\n")
	}
	if contextFocus != "" {
		prompt.WriteString("Task foundation:\n" + contextFocus + "\n\n")
	}

	client := llm.NewLMStudioClient(r.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stopIndicator := r.startLLMIndicatorIfAllowed("crawl summarize")
	defer stopIndicator()
	resp, err := client.Chat(ctx, llm.ChatRequest{
		Model:       r.cfg.LLM.Model,
		Temperature: 0.2,
		Messages: []llm.Message{
			{Role: "system", Content: "Return a concise, actionable summary. Do not ask the user to paste files; use the provided crawl index."},
			{Role: "user", Content: prompt.String()},
		},
	})
	if err != nil {
		r.recordLLMFailure(err)
		return err
	}
	r.recordLLMSuccess()

	outPath := filepath.Join(sessionDir, "artifacts", "web", "crawl-summary.md")
	_ = os.MkdirAll(filepath.Dir(outPath), 0o755)
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return nil
	}
	if err := os.WriteFile(outPath, []byte(content+"\n"), 0o644); err != nil {
		return err
	}
	logPath, _ := writeFSLog(sessionDir, "crawl-summary", "Summary-Path: "+outPath+"\nIndex: "+indexPath+"\nPages: "+fmt.Sprintf("%d", pages)+"\n")
	r.recordObservation("crawl_summary", []string{indexPath}, logPath, "summary="+outPath, nil)
	return nil
}

func parseCrawlOptions(args []string) (maxPages int, maxDepth int, sameHost bool, err error) {
	maxPages = crawlDefaultMaxPages
	maxDepth = crawlDefaultMaxDepth
	sameHost = true
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		lower := strings.ToLower(arg)
		if strings.HasPrefix(lower, "max_pages=") {
			v := strings.TrimSpace(strings.SplitN(arg, "=", 2)[1])
			n, convErr := strconv.Atoi(v)
			if convErr != nil || n <= 0 {
				return 0, 0, false, fmt.Errorf("invalid max_pages=%s", v)
			}
			maxPages = n
			continue
		}
		if strings.HasPrefix(lower, "max_depth=") {
			v := strings.TrimSpace(strings.SplitN(arg, "=", 2)[1])
			n, convErr := strconv.Atoi(v)
			if convErr != nil || n < 0 {
				return 0, 0, false, fmt.Errorf("invalid max_depth=%s", v)
			}
			maxDepth = n
			continue
		}
		if strings.HasPrefix(lower, "same_host=") {
			v := strings.TrimSpace(strings.SplitN(arg, "=", 2)[1])
			switch strings.ToLower(v) {
			case "true", "1", "yes", "y":
				sameHost = true
			case "false", "0", "no", "n":
				sameHost = false
			default:
				return 0, 0, false, fmt.Errorf("invalid same_host=%s", v)
			}
			continue
		}
	}
	return maxPages, maxDepth, sameHost, nil
}

func (r *Runner) fetchAndIndexPage(ctx context.Context, client *http.Client, sessionDir, pagesDir, url string, depth int) (crawlPage, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return crawlPage{URL: url, Depth: depth}, nil, err
	}
	req.Header.Set("User-Agent", "BirdHackBot/0.1 (+lab)")
	resp, err := client.Do(req)
	if err != nil {
		return crawlPage{URL: url, Depth: depth}, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, crawlBodyLimit))
	if err != nil {
		return crawlPage{URL: url, Depth: depth, Status: resp.StatusCode}, nil, err
	}
	contentType := resp.Header.Get("Content-Type")
	bodyPath, _ := writeCrawlBody(pagesDir, url, contentType, body)

	content := string(body)
	title := extractTitle(content)
	metaDescription := extractMetaDescription(content)
	headings := extractHeadings(content, 8)
	text := collapseWhitespace(stripHTML(stripScriptsAndStyles(content)))
	snippet := truncate(text, 2000)

	// Extract links (only if HTML-ish).
	links := []string{}
	if strings.Contains(strings.ToLower(contentType), "text/html") || strings.Contains(strings.ToLower(content), "<html") || strings.Contains(strings.ToLower(content), "<a ") {
		parsed, _ := neturl.Parse(url)
		links, _ = extractLinksHTML(body, parsed)
	}
	linksPath := ""
	if len(links) > 0 {
		linksPath, _ = writeCrawlLinks(pagesDir, url, links)
	}

	return crawlPage{
		URL:           url,
		Depth:         depth,
		Status:        resp.StatusCode,
		ContentType:   contentType,
		Title:         title,
		MetaDesc:      metaDescription,
		Headings:      headings,
		Snippet:       snippet,
		BodyPath:      bodyPath,
		LinksPath:     linksPath,
		DiscoveredOut: len(links),
	}, links, nil
}

func canonicalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func writeCrawlBody(pagesDir, url string, contentType string, body []byte) (string, error) {
	ext := ".bin"
	lowerCT := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(lowerCT, "text/html"):
		ext = ".html"
	case strings.Contains(lowerCT, "application/json"):
		ext = ".json"
	case strings.Contains(lowerCT, "text/plain"):
		ext = ".txt"
	}
	timestamp := time.Now().UTC().Format("20060102-150405.000000000")
	path := filepath.Join(pagesDir, fmt.Sprintf("page-%s%s", timestamp, ext))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func writeCrawlLinks(pagesDir, url string, links []string) (string, error) {
	timestamp := time.Now().UTC().Format("20060102-150405.000000000")
	path := filepath.Join(pagesDir, fmt.Sprintf("links-%s.txt", timestamp))
	builder := strings.Builder{}
	for _, link := range links {
		builder.WriteString(link)
		builder.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
