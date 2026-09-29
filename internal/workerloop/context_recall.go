package workerloop

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
		addMatch := func(ref, source, preview string, multiplier int) {
			score := 0
			lower := strings.ToLower(source)
			for _, term := range terms {
				if strings.Contains(lower, term) {
					score += multiplier
				}
			}
			if score > 0 {
				matches = append(matches, match{score, fmt.Sprintf("ref=%q preview=%q", ref, compactInline(preview, 240))})
			}
		}
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
		for i := len(packet.RecentConversation) - 1; i >= 0; i-- {
			addMatch(fmt.Sprintf("conversation:%d", i), packet.RecentConversation[i], packet.RecentConversation[i], 2)
		}
		for i := len(packet.PlanHistory) - 1; i >= 0; i-- {
			data, _ := json.Marshal(packet.PlanHistory[i])
			addMatch(fmt.Sprintf("plan:%d", i), string(data), packet.PlanHistory[i].Plan.Summary, 2)
		}
		if len(matches) == 0 {
			return ctxpacket.ContextRecall{Query: query, Stream: "index", Content: "No recorded material matched any query term; try a distinctive subject from the saved work."}, nil
		}
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
		if len(matches) > 5 {
			matches = matches[:5]
		}
		lines := make([]string, 0, len(matches))
		for _, item := range matches {
			lines = append(lines, item.line)
		}
		return ctxpacket.ContextRecall{Query: query, Stream: "index", Matched: true, Content: strings.Join(lines, "\n")}, nil
	}
	ref := strings.TrimSpace(request.ContextRef)
	if kind, indexText, ok := strings.Cut(ref, ":"); ok && (kind == "conversation" || kind == "plan") {
		if request.ContextStream != "" {
			return ctxpacket.ContextRecall{}, fmt.Errorf("saved %s does not have stdout or stderr streams", kind)
		}
		index, err := strconv.Atoi(indexText)
		if err != nil || index < 0 {
			return ctxpacket.ContextRecall{}, fmt.Errorf("invalid saved %s reference", kind)
		}
		var source string
		switch kind {
		case "conversation":
			if index >= len(packet.RecentConversation) {
				return ctxpacket.ContextRecall{}, fmt.Errorf("saved conversation reference is absent")
			}
			source = packet.RecentConversation[index]
		case "plan":
			if index >= len(packet.PlanHistory) {
				return ctxpacket.ContextRecall{}, fmt.Errorf("saved plan reference is absent")
			}
			data, err := json.Marshal(packet.PlanHistory[index])
			if err != nil {
				return ctxpacket.ContextRecall{}, err
			}
			source = string(data)
		}
		return recallSavedText(ref, kind, source, request.ContextOffset)
	}
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
	used := completeUTF8Prefix(buf[:n], request.ContextOffset+int64(n) < info.Size())
	content := string(buf[:used])
	if !utf8.ValidString(content) {
		content = strings.ToValidUTF8(content, "�") + "\n[non-UTF-8 bytes replaced; consult the original log]"
	}
	if used == 0 {
		content = "(end of recorded stream)"
	}
	return ctxpacket.ContextRecall{Ref: ref, Stream: stream, Matched: used > 0, Offset: request.ContextOffset, NextOffset: request.ContextOffset + int64(used), TotalBytes: info.Size(), Action: compactInline(source.Action, 512), ExitStatus: source.ExitStatus, Content: content}, nil
}

func recallSavedText(ref, kind, source string, offset int64) (ctxpacket.ContextRecall, error) {
	data := []byte(source)
	if offset > int64(len(data)) || offset < 0 {
		return ctxpacket.ContextRecall{}, fmt.Errorf("context offset exceeds saved %s length %d", kind, len(data))
	}
	start := int(offset)
	end := min(len(data), start+recallChunkBytes)
	used := completeUTF8Prefix(data[start:end], end < len(data))
	content := string(data[start : start+used])
	if used == 0 {
		content = "(end of saved material)"
	}
	return ctxpacket.ContextRecall{Ref: ref, Stream: kind, Matched: used > 0, Offset: offset, NextOffset: offset + int64(used), TotalBytes: int64(len(data)), Content: content}, nil
}

// If a valid multibyte rune straddles a page boundary, leave its bytes for
// the next read. Truly invalid bytes inside the page are handled by the caller.
func completeUTF8Prefix(data []byte, more bool) int {
	if !more {
		return len(data)
	}
	for i := 0; i < len(data); {
		_, width := utf8.DecodeRune(data[i:])
		if width == 1 && !utf8.FullRune(data[i:]) {
			return i
		}
		i += width
	}
	return len(data)
}
