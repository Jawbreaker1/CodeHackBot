package orchestrator

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type artifactRepairFamily string

const (
	artifactFamilyUnknown         artifactRepairFamily = "unknown"
	artifactFamilyInputSource     artifactRepairFamily = "input_source"
	artifactFamilyValidationInput artifactRepairFamily = "validation_input"
	artifactFamilyReportOutput    artifactRepairFamily = "report_output"
	artifactFamilyAuxiliaryLog    artifactRepairFamily = "auxiliary_log"
)

func repairMissingCommandInputPaths(cfg WorkerRunConfig, task TaskSpec, command string, args []string) ([]string, []string, bool, error) {
	if len(args) == 0 {
		return args, nil, false, nil
	}
	localTargetsOnly := taskTargetsLocalhostOnly(task)
	localWorkflow := taskLikelyLocalFileWorkflow(task)
	candidates := []string{}
	candidateSources := map[string]string{}
	wordlistCandidates, wordlistSources := collectWordlistCandidates(args)
	if len(wordlistCandidates) > 0 {
		candidates = append(candidates, wordlistCandidates...)
		for path, source := range wordlistSources {
			candidateSources[path] = source
		}
	}
	if len(task.DependsOn) > 0 {
		depCandidates, err := collectDependencyArtifactCandidates(cfg, task.DependsOn)
		if err != nil {
			return args, nil, false, err
		}
		candidates = append(candidates, depCandidates...)
	}
	shellCandidates := append([]string{}, candidates...)
	if localTargetsOnly {
		workspaceCandidates := collectWorkspaceCandidatesFromArgs(cfg, args)
		shellCandidates = append(workspaceCandidates, shellCandidates...)
	}
	if repairedArgs, repairNotes, repaired := repairMissingCommandInputPathsForShellWrapperWithTask(task, command, args, shellCandidates, candidateSources); repaired {
		return repairedArgs, appendUnique(nil, repairNotes...), true, nil
	}
	if !commandLikelyReadsLocalFiles(command) {
		return args, nil, false, nil
	}

	nextArgs := append([]string{}, args...)
	notes := []string{}
	changed := false
	used := map[string]struct{}{}
	resolveMissingPath := func(candidate string, expectedFamily artifactRepairFamily) (string, string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return "", ""
		}
		if pathContainsWildcard(candidate) {
			return "", ""
		}
		source := ""
		preferWorkspace := localTargetsOnly && (localWorkflow || pathLikelyWorkspaceInput(candidate))
		replacement := ""
		if preferWorkspace {
			replacement = bestWorkspaceCandidateForMissingPath(cfg, candidate)
			if replacement != "" && !artifactFamiliesCompatible(expectedFamily, classifyArtifactRepairFamily(replacement)) {
				replacement = ""
			}
			if replacement != "" {
				source = "local workspace"
			}
		}
		if replacement == "" {
			replacement = bestArtifactCandidateForMissingPathWithFamily(candidate, expectedFamily, candidates, used)
			if replacement != "" {
				if customSource, ok := candidateSources[replacement]; ok && strings.TrimSpace(customSource) != "" {
					source = customSource
				} else {
					source = "dependency artifact"
				}
			}
		}
		if replacement == "" && localTargetsOnly {
			replacement = bestWorkspaceCandidateForMissingPath(cfg, candidate)
			if replacement != "" && !artifactFamiliesCompatible(expectedFamily, classifyArtifactRepairFamily(replacement)) {
				replacement = ""
			}
			if replacement != "" {
				source = "local workspace"
			}
		}
		return replacement, source
	}

	for i, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if strings.HasPrefix(trimmed, "--wordlist=") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "--wordlist="))
			if value != "" {
				if _, statErr := os.Stat(value); statErr != nil {
					replacement, source := resolveMissingPath(value, artifactFamilyValidationInput)
					if replacement != "" {
						nextArgs[i] = "--wordlist=" + replacement
						used[replacement] = struct{}{}
						changed = true
						if source == "" {
							source = "fallback candidate"
						}
						notes = append(notes, fmt.Sprintf("runtime input repair: replaced missing path %s with %s %s", value, source, replacement))
					}
				}
			}
			continue
		}
		if !looksLikeFileInputArg(arg) {
			continue
		}
		candidate := trimmed
		if candidate == "" {
			continue
		}
		if _, statErr := os.Stat(candidate); statErr == nil {
			continue
		}
		expectedFamily := inferExpectedRepairFamily(task, command, args, i, candidate)
		replacement, source := resolveMissingPath(candidate, expectedFamily)
		if replacement == "" {
			continue
		}
		nextArgs[i] = replacement
		used[replacement] = struct{}{}
		changed = true
		if source == "" {
			source = "fallback candidate"
		}
		notes = append(notes, fmt.Sprintf("runtime input repair: replaced missing path %s with %s %s", candidate, source, replacement))
	}

	if !changed {
		return args, nil, false, nil
	}
	return nextArgs, appendUnique(nil, notes...), true, nil
}

func bestWorkspaceCandidateForMissingPath(cfg WorkerRunConfig, missingPath string) string {
	baseName := filepath.Base(strings.TrimSpace(missingPath))
	if baseName == "" || baseName == "." || baseName == ".." {
		return ""
	}
	for _, root := range localWorkspaceRoots(cfg) {
		candidate := filepath.Join(root, baseName)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		return candidate
	}
	return ""
}

func localWorkspaceRoots(cfg WorkerRunConfig) []string {
	roots := []string{}
	if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
		roots = append(roots, wd)
	}
	sessionsDir := strings.TrimSpace(cfg.SessionsDir)
	if sessionsDir != "" {
		abs := sessionsDir
		if !filepath.IsAbs(abs) {
			if resolved, err := filepath.Abs(abs); err == nil {
				abs = resolved
			}
		}
		if strings.TrimSpace(abs) != "" {
			roots = append(roots, filepath.Dir(abs))
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func taskLikelyLocalFileWorkflow(task TaskSpec) bool {
	if !taskTargetsLocalhostOnly(task) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		strings.Join(task.ExpectedArtifacts, " "),
	}, " ")))
	return containsAnySubstring(text, "archive", "zip", "password", "extract", "decrypt", "file", "crack", "proof", "token", "access")
}

func taskTargetsLocalhostOnly(task TaskSpec) bool {
	if len(task.Targets) == 0 {
		return true
	}
	for _, target := range task.Targets {
		normalized := strings.ToLower(strings.TrimSpace(target))
		if normalized == "" {
			continue
		}
		if normalized != "127.0.0.1" && normalized != "localhost" {
			return false
		}
	}
	return true
}

func pathLikelyWorkspaceInput(path string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	if base == "" || base == "." || base == ".." {
		return false
	}
	switch filepath.Ext(base) {
	case ".zip", ".7z", ".tar", ".gz", ".bz2", ".xz", ".tgz":
		return true
	}
	return strings.HasSuffix(base, ".hash")
}

func collectWorkspaceCandidatesFromArgs(cfg WorkerRunConfig, args []string) []string {
	candidates := []string{}
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if !looksLikeFileInputArg(trimmed) {
			continue
		}
		if candidate := bestWorkspaceCandidateForMissingPath(cfg, trimmed); candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	if body, ok := shellWrapperBody(args); ok {
		for _, match := range shellPathArgPattern.FindAllString(body, -1) {
			if !looksLikePathArg(match) {
				continue
			}
			if candidate := bestWorkspaceCandidateForMissingPath(cfg, match); candidate != "" {
				candidates = append(candidates, candidate)
			}
		}
		for _, token := range strings.Fields(body) {
			for _, candidatePath := range shellTokenFileCandidates(token) {
				if !looksLikeFileInputArg(candidatePath) {
					continue
				}
				if candidate := bestWorkspaceCandidateForMissingPath(cfg, candidatePath); candidate != "" {
					candidates = append(candidates, candidate)
				}
			}
		}
	}
	return appendUnique(nil, candidates...)
}

func shellWrapperBody(args []string) (string, bool) {
	if len(args) < 2 {
		return "", false
	}
	mode := strings.TrimSpace(args[0])
	if mode != "-c" && mode != "-lc" {
		return "", false
	}
	body := strings.TrimSpace(args[1])
	if body == "" {
		return "", false
	}
	return body, true
}

func collectWordlistCandidates(args []string) ([]string, map[string]string) {
	missingPaths := missingWordlistPathsFromArgs(args)
	if len(missingPaths) == 0 {
		return nil, map[string]string{}
	}
	candidates := []string{}
	sources := map[string]string{}
	for _, missingPath := range missingPaths {
		candidate, source := resolveWordlistCandidate(missingPath)
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		candidates = append(candidates, candidate)
		if strings.TrimSpace(source) != "" {
			sources[candidate] = source
		}
	}
	return appendUnique(nil, candidates...), sources
}

func missingWordlistPathsFromArgs(args []string) []string {
	paths := []string{}
	for _, arg := range args {
		for _, candidate := range wordlistPathCandidatesFromArg(arg) {
			if _, statErr := os.Stat(candidate); statErr != nil {
				paths = append(paths, candidate)
			}
		}
	}
	if body, ok := shellWrapperBody(args); ok {
		for _, path := range shellPathArgPattern.FindAllString(body, -1) {
			candidate := strings.TrimSpace(path)
			if !looksLikeWordlistPath(candidate) {
				continue
			}
			if _, statErr := os.Stat(candidate); statErr != nil {
				paths = append(paths, candidate)
			}
		}
	}
	return appendUnique(nil, paths...)
}

func wordlistPathCandidatesFromArg(arg string) []string {
	candidates := []string{}
	candidate := strings.TrimSpace(arg)
	if looksLikeWordlistPath(candidate) && looksLikeFileInputArg(candidate) {
		candidates = append(candidates, candidate)
	}
	if strings.HasPrefix(candidate, "--wordlist=") {
		value := strings.TrimSpace(strings.TrimPrefix(candidate, "--wordlist="))
		if looksLikeWordlistPath(value) && looksLikeFileInputArg(value) {
			candidates = append(candidates, value)
		}
	}
	return appendUnique(nil, candidates...)
}

func looksLikeWordlistPath(path string) bool {
	normalized := strings.ToLower(strings.TrimSpace(path))
	if normalized == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(normalized))
	if strings.Contains(normalized, "/wordlist") || strings.Contains(normalized, "/dict/") {
		return true
	}
	if strings.HasSuffix(base, ".lst") || strings.HasSuffix(base, ".dic") {
		return true
	}
	if strings.HasSuffix(base, ".txt") && (strings.Contains(base, "rockyou") || strings.Contains(base, "word") || strings.Contains(base, "pass")) {
		return true
	}
	return false
}

func resolveWordlistCandidate(missingPath string) (string, string) {
	requested := strings.TrimSpace(missingPath)
	if requested == "" {
		return "", ""
	}
	if info, err := os.Stat(requested); err == nil && !info.IsDir() {
		return requested, "local wordlist"
	}
	localArchive := requested + ".gz"
	if info, err := os.Stat(localArchive); err == nil && !info.IsDir() {
		path, decompressErr := ensureDecompressedWordlist(requested, localArchive)
		if decompressErr == nil {
			return path, "decompressed local wordlist archive"
		}
	}
	baseName := strings.TrimSpace(filepath.Base(requested))
	if baseName == "" || baseName == "." || baseName == ".." {
		return "", ""
	}
	systemWordlist := filepath.Join("/usr/share/wordlists", baseName)
	if info, err := os.Stat(systemWordlist); err == nil && !info.IsDir() {
		return systemWordlist, "system wordlist"
	}
	systemArchive := systemWordlist + ".gz"
	if info, err := os.Stat(systemArchive); err == nil && !info.IsDir() {
		path, decompressErr := ensureDecompressedWordlist(systemWordlist, systemArchive)
		if decompressErr == nil {
			return path, "decompressed system wordlist archive"
		}
	}
	return "", ""
}

func ensureDecompressedWordlist(requestedPath, archivePath string) (string, error) {
	wordlistDir := filepath.Join(os.TempDir(), "birdhackbot-wordlists")
	if err := os.MkdirAll(wordlistDir, 0o755); err != nil {
		return "", err
	}
	outName := strings.TrimSpace(filepath.Base(requestedPath))
	if outName == "" || outName == "." || outName == ".." {
		outName = strings.TrimSuffix(filepath.Base(strings.TrimSpace(archivePath)), ".gz")
	}
	if strings.HasSuffix(strings.ToLower(outName), ".gz") {
		outName = strings.TrimSuffix(outName, ".gz")
	}
	if outName == "" || outName == "." || outName == ".." {
		outName = "wordlist.txt"
	}
	outPath := filepath.Join(wordlistDir, outName)
	if info, err := os.Stat(outPath); err == nil && !info.IsDir() && info.Size() > 0 {
		return outPath, nil
	}
	src, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer src.Close()
	reader, err := gzip.NewReader(src)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	tmpPath := fmt.Sprintf("%s.tmp-%d", outPath, time.Now().UTC().UnixNano())
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	limited := &io.LimitedReader{R: reader, N: wordlistDecompressMaxBytes + 1}
	n, copyErr := io.Copy(dst, limited)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", closeErr
	}
	if n > wordlistDecompressMaxBytes {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("decompressed wordlist exceeds limit")
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return outPath, nil
}

func repairMissingCommandInputPathsForShellWrapper(command string, args []string, candidates []string, candidateSources map[string]string) ([]string, []string, bool) {
	return repairMissingCommandInputPathsForShellWrapperWithTask(TaskSpec{}, command, args, candidates, candidateSources)
}

func repairMissingCommandInputPathsForShellWrapperWithTask(task TaskSpec, command string, args []string, candidates []string, candidateSources map[string]string) ([]string, []string, bool) {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if base != "bash" && base != "sh" && base != "zsh" {
		return args, nil, false
	}
	body, ok := shellWrapperBody(args)
	if !ok || !shellScriptLikelyReadsLocalFiles(body) {
		return args, nil, false
	}

	changedBody := body
	notes := []string{}
	changed := false
	used := map[string]struct{}{}
	fields := strings.Fields(body)
	searchPos := 0
	skipNextAsOutputPath := false

	for _, field := range fields {
		token := strings.TrimSpace(field)
		if token == "" {
			continue
		}
		if isOutputRedirectionOperatorToken(token) {
			skipNextAsOutputPath = true
			continue
		}
		if skipNextAsOutputPath {
			skipNextAsOutputPath = false
			continue
		}
		if tokenStartsWithOutputRedirection(token) {
			continue
		}

		tokenStart, found := findNextShellTokenOccurrence(changedBody, token, searchPos)
		currentToken := token
		for _, match := range shellPathArgPattern.FindAllStringIndex(currentToken, -1) {
			if len(match) != 2 {
				continue
			}
			start := match[0]
			end := match[1]
			if start < 0 || end <= start || end > len(currentToken) {
				continue
			}
			if !isShellPathMatchBoundary(currentToken, start) {
				continue
			}
			candidate := currentToken[start:end]
			if !looksLikePathArg(candidate) {
				continue
			}
			if _, statErr := os.Stat(candidate); statErr == nil {
				continue
			}
			if pathContainsWildcard(candidate) {
				continue
			}
			expectedFamily := inferExpectedRepairFamily(task, command, args, 1, candidate)
			replacement := bestArtifactCandidateForMissingPathWithFamily(candidate, expectedFamily, candidates, used)
			if replacement == "" {
				continue
			}
			replacementToken := replacement
			if !tokenContainsQuotedPath(currentToken, candidate) {
				replacementToken = shellQuotePath(replacement)
			}
			nextToken := strings.Replace(currentToken, candidate, replacementToken, 1)
			if nextToken == currentToken {
				continue
			}
			currentToken = nextToken
			used[replacement] = struct{}{}
			changed = true
			source := "dependency artifact"
			if customSource, ok := candidateSources[replacement]; ok && strings.TrimSpace(customSource) != "" {
				source = customSource
			}
			notes = append(notes, fmt.Sprintf("runtime input repair: replaced missing path %s with %s %s", candidate, source, replacement))
		}

		for _, bareCandidate := range shellTokenFileCandidates(currentToken) {
			if !looksLikeFileInputArg(bareCandidate) {
				continue
			}
			if _, statErr := os.Stat(bareCandidate); statErr == nil {
				continue
			}
			if pathContainsWildcard(bareCandidate) {
				continue
			}
			expectedFamily := inferExpectedRepairFamily(task, command, args, 1, bareCandidate)
			replacement := bestArtifactCandidateForMissingPathWithFamily(bareCandidate, expectedFamily, candidates, used)
			if replacement == "" {
				continue
			}
			replacementToken := replacement
			if !tokenContainsQuotedPath(currentToken, bareCandidate) {
				replacementToken = shellQuotePath(replacement)
			}
			nextToken := strings.Replace(currentToken, bareCandidate, replacementToken, 1)
			if nextToken == currentToken {
				continue
			}
			currentToken = nextToken
			used[replacement] = struct{}{}
			changed = true
			source := "dependency artifact"
			if customSource, ok := candidateSources[replacement]; ok && strings.TrimSpace(customSource) != "" {
				source = customSource
			}
			notes = append(notes, fmt.Sprintf("runtime input repair: replaced missing path %s with %s %s", bareCandidate, source, replacement))
		}
		if found {
			if currentToken != token {
				changedBody = changedBody[:tokenStart] + currentToken + changedBody[tokenStart+len(token):]
			}
			searchPos = tokenStart + len(currentToken)
		}
	}

	if !changed {
		return args, nil, false
	}
	nextArgs := append([]string{}, args...)
	nextArgs[1] = changedBody
	return nextArgs, notes, true
}

func findNextShellTokenOccurrence(body, token string, start int) (int, bool) {
	if strings.TrimSpace(token) == "" {
		return 0, false
	}
	if start < 0 {
		start = 0
	}
	for offset := start; offset < len(body); {
		idx := strings.Index(body[offset:], token)
		if idx < 0 {
			return 0, false
		}
		idx += offset
		if isShellTokenBoundary(body, idx-1) && isShellTokenBoundary(body, idx+len(token)) {
			return idx, true
		}
		offset = idx + 1
	}
	return 0, false
}

func isShellTokenBoundary(body string, index int) bool {
	if index < 0 || index >= len(body) {
		return true
	}
	switch body[index] {
	case ' ', '\t', '\n', '\r', ';', '&', '|', '>', '<', '(', ')':
		return true
	default:
		return false
	}
}

func shellTokenFileCandidates(token string) []string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return nil
	}
	filter := func(raw string) string {
		candidate := strings.Trim(raw, `"'`)
		if candidate == "" {
			return ""
		}
		// Skip embedded relative segments (for example extracted_secret/secret_text.txt)
		// because they are often script-local literals, not missing dependency inputs.
		if strings.Contains(candidate, "/") && !looksLikePathArg(candidate) {
			return ""
		}
		return candidate
	}
	candidates := []string{filter(trimmed)}
	if strings.HasPrefix(trimmed, "--") && strings.Contains(trimmed, "=") {
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 {
			candidates = append(candidates, filter(parts[1]))
		}
	}
	return appendUnique(nil, compactStringSlice(candidates)...)
}

func shellScriptLikelyReadsLocalFiles(body string) bool {
	for _, token := range shellCommandTokens(body) {
		if commandLikelyReadsLocalFiles(token) {
			return true
		}
	}
	return false
}

func isOutputRedirectionOperatorToken(token string) bool {
	switch strings.TrimSpace(token) {
	case ">", ">>", "1>", "1>>", "2>", "2>>", "&>", "&>>":
		return true
	default:
		return false
	}
}

func tokenStartsWithOutputRedirection(token string) bool {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, ">") ||
		strings.HasPrefix(trimmed, "1>") ||
		strings.HasPrefix(trimmed, "2>") ||
		strings.HasPrefix(trimmed, "&>")
}

func tokenContainsQuotedPath(token, path string) bool {
	return strings.Contains(token, `"`+path+`"`) || strings.Contains(token, `'`+path+`'`)
}

func shellQuotePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return path
	}
	if !strings.ContainsAny(trimmed, " \t\n\r\"'`$&;|<>") {
		return trimmed
	}
	escaped := strings.ReplaceAll(trimmed, `'`, `'"'"'`)
	return "'" + escaped + "'"
}

func isShellPathMatchBoundary(token string, start int) bool {
	if start <= 0 || start >= len(token) {
		return true
	}
	prev := rune(token[start-1])
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev) && prev != '_' && prev != '-' && prev != '.'
}

func commandLikelyReadsLocalFiles(command string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "7z", "awk", "cat", "cut", "egrep", "fcrackzip", "fgrep", "file", "grep", "head", "john", "jq", "less", "list_dir", "ls", "more", "read_file", "sed", "sort", "stat", "tail", "uniq", "unzip", "wc", "zip2john", "zipinfo":
		return true
	default:
		return false
	}
}

func looksLikePathArg(arg string) bool {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../")
}

func looksLikeFileInputArg(arg string) bool {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "-") {
		return false
	}
	if strings.Contains(trimmed, "://") {
		return false
	}
	if strings.ContainsAny(trimmed, "|&;<>`$") {
		return false
	}
	if strings.EqualFold(trimmed, "localhost") {
		return false
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		return false
	}
	if looksLikePathArg(trimmed) {
		return true
	}
	if strings.Contains(trimmed, "/") {
		return true
	}
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(trimmed)))
	if ext == "" {
		return false
	}
	switch ext {
	case ".com", ".net", ".org", ".io", ".local", ".lan":
		return false
	default:
		return true
	}
}

func collectDependencyArtifactCandidates(cfg WorkerRunConfig, dependencies []string) ([]string, error) {
	base := BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir
	candidates := []string{}
	for _, dep := range compactStringSlice(dependencies) {
		depDir := filepath.Join(base, dep)
		info, err := os.Stat(depDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			continue
		}
		if err := filepath.WalkDir(depDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			candidates = append(candidates, referencedExistingPathsFromArtifact(path)...)
			candidates = append(candidates, path)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return appendUnique(nil, candidates...), nil
}

func referencedExistingPathsFromArtifact(path string) []string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > dependencyArtifactReferenceMaxBytes {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	candidates := []string{}
	text := string(content)
	for _, match := range artifactPathLinePattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		if resolved := normalizeArtifactPathReference(match[1]); resolved != "" {
			candidates = append(candidates, resolved)
		}
	}
	for _, match := range artifactJSONPathPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		if resolved := normalizeArtifactPathReference(match[1]); resolved != "" {
			candidates = append(candidates, resolved)
		}
	}
	return appendUnique(nil, candidates...)
}

func normalizeArtifactPathReference(raw string) string {
	candidate := strings.TrimSpace(raw)
	candidate = strings.Trim(candidate, `"'`)
	candidate = strings.TrimRight(candidate, ",")
	if candidate == "" || !filepath.IsAbs(candidate) {
		return ""
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return ""
	}
	return candidate
}

func bestArtifactCandidateForMissingPath(missingPath string, candidates []string, used map[string]struct{}) string {
	return bestArtifactCandidateForMissingPathWithFamily(missingPath, inferExpectedRepairFamily(TaskSpec{}, "", nil, 0, missingPath), candidates, used)
}

func bestArtifactCandidateForMissingPathWithFamily(missingPath string, expectedFamily artifactRepairFamily, candidates []string, used map[string]struct{}) string {
	if pathContainsWildcard(missingPath) {
		return ""
	}
	missingBase := strings.ToLower(strings.TrimSpace(filepath.Base(missingPath)))
	if missingBase == "" {
		return ""
	}

	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(filepath.Base(candidate)), missingBase) {
			return candidate
		}
		if _, alreadyUsed := used[candidate]; alreadyUsed {
			continue
		}
		if expectedFamily != artifactFamilyUnknown && !artifactFamiliesCompatible(expectedFamily, classifyArtifactRepairFamily(candidate)) {
			continue
		}
	}
	if expectedFamily == artifactFamilyUnknown {
		return ""
	}

	missingTokens := tokenizeArtifactHint(missingBase)
	if len(missingTokens) == 0 {
		return ""
	}
	bestPath := ""
	bestScore := 0
	bestSpecificity := -1
	for _, candidate := range candidates {
		if _, alreadyUsed := used[candidate]; alreadyUsed {
			continue
		}
		if !artifactFamiliesCompatible(expectedFamily, classifyArtifactRepairFamily(candidate)) {
			continue
		}
		candidateTokens := tokenizeArtifactHint(filepath.Base(candidate))
		if len(candidateTokens) == 0 {
			continue
		}
		score := artifactTokenMatchScore(missingTokens, candidateTokens)
		specificity := artifactCandidateSpecificity(candidate, candidateTokens)
		if score < bestScore {
			continue
		}
		if score == bestScore && specificity <= bestSpecificity {
			continue
		}
		bestScore = score
		bestSpecificity = specificity
		bestPath = candidate
	}
	if bestScore < 2 {
		return ""
	}
	return bestPath
}

type pathArgumentRole string

const (
	pathArgRoleUnknown pathArgumentRole = "unknown"
	pathArgRoleInput   pathArgumentRole = "input"
	pathArgRoleOutput  pathArgumentRole = "output"
)

func inferExpectedRepairFamily(task TaskSpec, command string, args []string, argIndex int, path string) artifactRepairFamily {
	role := pathArgRole(command, args, argIndex, path)
	semanticFamily := inferFamilyFromArgSemantics(command, args, argIndex, path)
	if intentFamily, ok := inferFamilyFromTaskIntent(task, command, path, role); ok {
		// Keep intent-first behavior, but allow explicit arg semantics
		// (wordlist/log/output flags) to refine family for this argument.
		if semanticFamily == artifactFamilyReportOutput || semanticFamily == artifactFamilyAuxiliaryLog {
			return semanticFamily
		}
		if semanticFamily == artifactFamilyValidationInput && looksLikeWordlistPath(path) {
			return semanticFamily
		}
		return intentFamily
	}
	if semanticFamily != artifactFamilyUnknown {
		return semanticFamily
	}
	if role == pathArgRoleOutput {
		if looksLikeLogPath(path) {
			return artifactFamilyAuxiliaryLog
		}
		return artifactFamilyReportOutput
	}
	if family := classifyArtifactRepairFamily(path); family != artifactFamilyUnknown {
		return family
	}
	return artifactFamilyUnknown
}

func inferFamilyFromTaskIntent(task TaskSpec, command, path string, role pathArgumentRole) (artifactRepairFamily, bool) {
	intentText := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		strings.Join(task.DoneWhen, " "),
		strings.Join(task.FailWhen, " "),
		strings.Join(task.ExpectedArtifacts, " "),
		strings.ToLower(strings.TrimSpace(filepath.Base(command))),
	}, " ")))
	if strings.TrimSpace(intentText) == "" {
		return artifactFamilyUnknown, false
	}
	if role == pathArgRoleOutput {
		if containsAnySubstring(intentText, "log", "trace", "journal", "debug", "stdout", "stderr") {
			return artifactFamilyAuxiliaryLog, true
		}
		return artifactFamilyReportOutput, true
	}
	if taskLikelyLocalFileWorkflow(task) && classifyArtifactRepairFamily(path) == artifactFamilyInputSource {
		return artifactFamilyInputSource, true
	}
	if containsAnySubstring(intentText,
		"report", "owasp", "summary", "synthesis", "writeup", "finding", "remediation",
	) {
		return artifactFamilyValidationInput, true
	}
	if containsAnySubstring(intentText,
		"validate", "validation", "verification", "verify", "proof",
		"fingerprint", "hash", "cve", "evidence", "cross-reference",
	) {
		return artifactFamilyValidationInput, true
	}
	if containsAnySubstring(intentText,
		"extract", "decrypt", "decrypting", "archive", "zip", "7z", "rar", "tar",
		"password", "credential", "token", "access", "scan", "probe", "enumerate",
		"discover", "recon", "inventory",
	) {
		return artifactFamilyInputSource, true
	}
	// If the task intent is local file centric and the path looks file-like, prefer input source.
	if taskLikelyLocalFileWorkflow(task) && looksLikePathArg(path) {
		return artifactFamilyInputSource, true
	}
	return artifactFamilyUnknown, false
}

func inferFamilyFromArgSemantics(command string, args []string, argIndex int, path string) artifactRepairFamily {
	role := pathArgRole(command, args, argIndex, path)
	if role == pathArgRoleOutput {
		if looksLikeLogPath(path) {
			return artifactFamilyAuxiliaryLog
		}
		return artifactFamilyReportOutput
	}
	if looksLikeLogPath(path) {
		return artifactFamilyAuxiliaryLog
	}
	if looksLikeWordlistPath(path) {
		return artifactFamilyValidationInput
	}
	trimmed := strings.ToLower(strings.TrimSpace(path))
	if strings.Contains(trimmed, "wordlist") || strings.Contains(trimmed, "rockyou") {
		return artifactFamilyValidationInput
	}
	if argIndex >= 0 && argIndex < len(args) {
		arg := strings.ToLower(strings.TrimSpace(args[argIndex]))
		switch {
		case strings.HasPrefix(arg, "--wordlist="),
			arg == "--wordlist",
			arg == "-w",
			strings.Contains(arg, "wordlist"):
			return artifactFamilyValidationInput
		case strings.HasPrefix(arg, "--report="),
			strings.HasPrefix(arg, "--output="),
			strings.HasPrefix(arg, "--outfile="):
			return artifactFamilyReportOutput
		case strings.HasPrefix(arg, "--log="),
			strings.HasPrefix(arg, "--log-file="):
			return artifactFamilyAuxiliaryLog
		}
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if containsAnySubstring(base, "report", "summarize", "synth") {
		return artifactFamilyValidationInput
	}
	return artifactFamilyUnknown
}

func pathArgRole(command string, args []string, argIndex int, path string) pathArgumentRole {
	if strings.TrimSpace(path) == "" {
		return pathArgRoleUnknown
	}
	if argIndex >= 0 && argIndex < len(args) {
		arg := strings.ToLower(strings.TrimSpace(args[argIndex]))
		if strings.HasPrefix(arg, "--output=") ||
			strings.HasPrefix(arg, "--outfile=") ||
			strings.HasPrefix(arg, "--report=") ||
			strings.HasPrefix(arg, "--result=") ||
			strings.HasPrefix(arg, "--log=") ||
			strings.HasPrefix(arg, "--log-file=") {
			return pathArgRoleOutput
		}
		if arg == "-o" || arg == "--output" || arg == "--outfile" || arg == "--report" || arg == "--log" || arg == "--log-file" {
			return pathArgRoleOutput
		}
	}
	if argIndex > 0 && argIndex < len(args) {
		prev := strings.ToLower(strings.TrimSpace(args[argIndex-1]))
		switch prev {
		case "-o", "--output", "--outfile", "--report", "--log", "--log-file":
			return pathArgRoleOutput
		}
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if base == "report" && argIndex >= 0 {
		// Builtin report command usually takes output-path style arguments.
		return pathArgRoleOutput
	}
	return pathArgRoleInput
}

func classifyArtifactRepairFamily(path string) artifactRepairFamily {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return artifactFamilyUnknown
	}
	lower := strings.ToLower(trimmed)
	base := strings.ToLower(strings.TrimSpace(filepath.Base(trimmed)))
	if looksLikeWordlistPath(trimmed) {
		return artifactFamilyValidationInput
	}
	if strings.Contains(lower, "wordlist") || strings.Contains(lower, "rockyou") {
		return artifactFamilyValidationInput
	}
	if strings.Contains(base, "wordlist") || strings.Contains(base, "rockyou") {
		return artifactFamilyValidationInput
	}
	if strings.Contains(base, "report") || strings.Contains(base, "owasp") {
		return artifactFamilyReportOutput
	}
	if strings.Contains(base, "log") {
		return artifactFamilyAuxiliaryLog
	}
	if strings.Contains(base, "hash") || strings.HasSuffix(base, ".pot") {
		return artifactFamilyValidationInput
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".zip", ".7z", ".rar", ".tar", ".tgz", ".gz", ".bz2", ".xz":
		if strings.Contains(base, "wordlist") || strings.Contains(base, "rockyou") {
			return artifactFamilyValidationInput
		}
		return artifactFamilyInputSource
	case ".hash", ".pot":
		return artifactFamilyValidationInput
	case ".lst", ".dic":
		return artifactFamilyValidationInput
	case ".log", ".jsonl":
		return artifactFamilyAuxiliaryLog
	case ".md", ".html", ".htm", ".pdf":
		return artifactFamilyReportOutput
	}
	if strings.Contains(lower, "/report") || strings.Contains(lower, "/reports/") {
		return artifactFamilyReportOutput
	}
	if strings.Contains(lower, "/logs/") {
		return artifactFamilyAuxiliaryLog
	}
	if looksLikeLogPath(path) {
		return artifactFamilyAuxiliaryLog
	}
	if looksLikePathArg(path) {
		return artifactFamilyInputSource
	}
	return artifactFamilyUnknown
}

func artifactFamiliesCompatible(expected, candidate artifactRepairFamily) bool {
	if expected == artifactFamilyUnknown {
		// unknown family allows exact-basename substitution only;
		// semantic matching is blocked in candidate selector.
		return true
	}
	if candidate == artifactFamilyUnknown {
		return false
	}
	return expected == candidate
}

func pathContainsWildcard(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	return strings.ContainsAny(trimmed, "*?[]")
}

func looksLikeLogPath(path string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(path))
	if trimmed == "" {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(trimmed)))
	if strings.Contains(base, "log") || strings.Contains(trimmed, "/logs/") {
		return true
	}
	switch filepath.Ext(base) {
	case ".log", ".jsonl", ".trace":
		return true
	default:
		return false
	}
}

func tokenizeArtifactHint(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if len(token) < 3 {
			continue
		}
		out = append(out, token)
	}
	return appendUnique(nil, out...)
}

func artifactTokenMatchScore(needles, haystack []string) int {
	score := 0
	for _, needle := range needles {
		bestTokenScore := 0
		for _, candidate := range haystack {
			if needle == candidate {
				if isGenericArtifactHintToken(needle) {
					bestTokenScore = max(bestTokenScore, 1)
				} else {
					bestTokenScore = max(bestTokenScore, 4)
				}
				continue
			}
			if commonPrefixLen(needle, candidate) >= 4 {
				if isGenericArtifactHintToken(needle) || isGenericArtifactHintToken(candidate) {
					bestTokenScore = max(bestTokenScore, 1)
				} else {
					bestTokenScore = max(bestTokenScore, 2)
				}
			}
		}
		score += bestTokenScore
	}
	score += artifactSemanticOverlapScore(needles, haystack)
	return score
}

func artifactCandidateSpecificity(path string, tokens []string) int {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	score := 0
	for _, token := range tokens {
		if isGenericArtifactHintToken(token) {
			continue
		}
		score++
	}
	if strings.HasPrefix(base, "worker-") && strings.HasSuffix(base, ".log") {
		score -= 2
	}
	return score
}

func isGenericArtifactHintToken(token string) bool {
	_, ok := genericArtifactHintTokens[strings.ToLower(strings.TrimSpace(token))]
	return ok
}

func artifactSemanticOverlapScore(needles, haystack []string) int {
	needleGroups := semanticGroupsForTokens(needles)
	if len(needleGroups) == 0 {
		return 0
	}
	candidateGroups := semanticGroupsForTokens(haystack)
	if len(candidateGroups) == 0 {
		return 0
	}
	score := 0
	for group := range needleGroups {
		if _, ok := candidateGroups[group]; !ok {
			continue
		}
		if group == "vuln" {
			score += 2
			continue
		}
		if group == "scan" {
			score += 2
			continue
		}
		score++
	}
	return score
}

func semanticGroupsForTokens(tokens []string) map[string]struct{} {
	if len(tokens) == 0 {
		return nil
	}
	groups := map[string]struct{}{}
	for _, token := range tokens {
		group := semanticGroupForToken(token)
		if group == "" {
			continue
		}
		groups[group] = struct{}{}
	}
	if len(groups) == 0 {
		return nil
	}
	return groups
}

func semanticGroupForToken(token string) string {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "scan", "service", "version", "port", "host", "nmap", "inventory", "discovery", "enum", "enumeration":
		return "scan"
	case "vuln", "vulnerability", "cve", "exploit", "misconfig", "misconfiguration", "exposure":
		return "vuln"
	case "report", "owasp", "finding", "evidence", "remediation", "summary":
		return "report"
	default:
		return ""
	}
}

func commonPrefixLen(a, b string) int {
	maxLen := min(len(a), len(b))
	count := 0
	for i := 0; i < maxLen; i++ {
		if a[i] != b[i] {
			break
		}
		count++
	}
	return count
}
