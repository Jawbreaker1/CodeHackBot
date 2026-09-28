package workerloop

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
)

const recallChunkBytes = 8192

func registeredContextRefs(packet ctxpacket.WorkerPacket, requested []string) []string {
	results := append([]ctxpacket.ExecutionResult{packet.LatestExecutionResult}, packet.RelevantRecentResults...)
	kept := make([]string, 0, len(requested))
	for _, ref := range requested {
		if _, found := recordedResult(results, ref); found {
			kept = append(kept, ref)
		}
	}
	return kept
}

func recordedResult(results []ctxpacket.ExecutionResult, ref string) (ctxpacket.ExecutionResult, bool) {
	for _, result := range results {
		for _, saved := range result.LogRefs {
			if saved == ref {
				return result, true
			}
		}
	}
	return ctxpacket.ExecutionResult{}, false
}

// recallContext searches the authoritative execution history or reads a slice
// of a registered command log. It never runs a command or accepts a new path.
func (l Loop) recallContext(packet ctxpacket.WorkerPacket, request Response) (ctxpacket.ContextRecall, error) {
	results := make([]ctxpacket.ExecutionResult, 0, 1+len(packet.RelevantRecentResults))
	if packet.LatestExecutionResult.Action != "" {
		results = append(results, packet.LatestExecutionResult)
	}
	results = append(results, packet.RelevantRecentResults...)
	if request.ContextQuery != "" {
		query := strings.TrimSpace(request.ContextQuery)
		if len(query) > 200 {
			return ctxpacket.ContextRecall{}, fmt.Errorf("context query exceeds 200 bytes")
		}
		terms := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
		type match struct {
			score int
			line  string
		}
		matches := make([]match, 0, 5)
		for _, result := range results {
			if len(result.LogRefs) == 0 {
				continue
			}
			action := strings.ToLower(result.Action)
			summary := strings.ToLower(result.OutputSummary + " " + result.OutputEvidence)
			refText := strings.ToLower(strings.Join(result.LogRefs, " "))
			score := 0
			for _, term := range terms {
				if strings.Contains(action, term) {
					score += 3
				}
				if strings.Contains(summary, term) {
					score += 2
				}
				if strings.Contains(refText, term) {
					score++
				}
			}
			if score > 0 {
				matches = append(matches, match{score, fmt.Sprintf("ref=%q action=%q summary=%q", result.LogRefs[0], compactInline(result.Action, 160), compactInline(result.OutputSummary, 240))})
			}
		}
		if len(matches) == 0 {
			return ctxpacket.ContextRecall{Query: query, Stream: "index", Content: "No recorded result matched any query term; try a subject or command name from the saved work."}, nil
		}
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
		if len(matches) > 5 {
			matches = matches[:5]
		}
		lines := make([]string, 0, len(matches))
		for _, item := range matches {
			lines = append(lines, item.line)
		}
		return ctxpacket.ContextRecall{Query: query, Stream: "index", Content: strings.Join(lines, "\n")}, nil
	}
	ref := strings.TrimSpace(request.ContextRef)
	source, found := recordedResult(results, ref)
	if !found {
		return ctxpacket.ContextRecall{}, fmt.Errorf("context reference is not a recorded command log for this worker")
	}
	logDir, err := filepath.Abs(l.Executor.LogDir)
	if err != nil {
		return ctxpacket.ContextRecall{}, err
	}
	path, err := filepath.Abs(ref)
	if err != nil {
		return ctxpacket.ContextRecall{}, err
	}
	rel, err := filepath.Rel(logDir, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ctxpacket.ContextRecall{}, fmt.Errorf("recorded command log is outside this worker's log directory")
	}
	stream := request.ContextStream
	if stream == "" {
		stream = "stdout"
	}
	path += "." + stream
	info, err := os.Lstat(path)
	if err != nil {
		return ctxpacket.ContextRecall{}, fmt.Errorf("open recorded %s: %w", stream, err)
	}
	if !info.Mode().IsRegular() {
		return ctxpacket.ContextRecall{}, fmt.Errorf("recorded %s is not a regular file", stream)
	}
	if request.ContextOffset > info.Size() {
		return ctxpacket.ContextRecall{}, fmt.Errorf("context offset exceeds recorded %s length %d", stream, info.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return ctxpacket.ContextRecall{}, err
	}
	defer f.Close()
	buf := make([]byte, recallChunkBytes)
	n, err := f.ReadAt(buf, request.ContextOffset)
	if err != nil && err != io.EOF {
		return ctxpacket.ContextRecall{}, err
	}
	content := string(buf[:n])
	if !utf8.ValidString(content) {
		content = strings.ToValidUTF8(content, "�") + "\n[non-UTF-8 bytes replaced; consult the original log]"
	}
	if n == 0 {
		content = "(end of recorded stream)"
	}
	return ctxpacket.ContextRecall{Ref: ref, Stream: stream, Offset: request.ContextOffset, TotalBytes: info.Size(), Action: compactInline(source.Action, 512), ExitStatus: source.ExitStatus, Content: content}, nil
}
