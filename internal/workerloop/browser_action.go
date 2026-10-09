package workerloop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/reporoot"
)

// prepareBrowserRun turns the browser-specific decision into the same exact
// command and artifact contract used by bash. No browser runs outside execute.
func prepareBrowserRun(response Response, cwd string) (Response, *ctxpacket.ExecutionResult) {
	root, err := filepath.Abs(cwd)
	if err != nil {
		return response, validationFailure(response.Scenario, "worker workspace could not be resolved", "invalid_action", "invalid_action")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return response, validationFailure(response.Scenario, "worker workspace is unavailable", "invalid_action", "invalid_action")
	}
	if filepath.IsAbs(response.Scenario) {
		return response, validationFailure(response.Scenario, "browser scenario must be relative to the worker workspace", "invalid_action", "invalid_action")
	}
	scenario := filepath.Join(root, response.Scenario)
	resolved, err := filepath.EvalSymlinks(scenario)
	if err != nil || !withinWorkspace(root, resolved) || filepath.Ext(resolved) != ".mjs" {
		return response, validationFailure(response.Scenario, "browser scenario must be an existing .mjs file in the worker workspace", "invalid_action", "invalid_action")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return response, validationFailure(response.Scenario, "browser scenario is not a regular file", "invalid_action", "invalid_action")
	}
	dir := strings.TrimSpace(response.ArtifactDir)
	if dir == "." || dir == ".." || filepath.Base(dir) != dir || filepath.IsAbs(dir) {
		return response, validationFailure(dir, "browser artifact_dir must be a new directory name in the worker workspace", "invalid_action", "invalid_action")
	}
	if _, err := os.Lstat(filepath.Join(root, dir)); err == nil || !os.IsNotExist(err) {
		return response, validationFailure(dir, "browser artifact_dir already exists or cannot be checked; choose a new name", "invalid_action", "invalid_action")
	}
	project, err := reporoot.Find(root)
	if err != nil {
		return response, validationFailure(response.Scenario, "browser runner could not be found from the worker workspace", "not_executable", "not_executable")
	}
	runner := filepath.Join(project, "tools", "playwright-runner", "run.mjs")
	if info, err := os.Stat(runner); err != nil || !info.Mode().IsRegular() {
		return response, validationFailure(runner, "browser runner is unavailable", "not_executable", "not_executable")
	}
	if _, err := exec.LookPath("node"); err != nil {
		return response, validationFailure("node", "Node.js is unavailable", "not_executable", "not_executable")
	}
	if len(response.Artifacts) > 5 {
		return response, validationFailure(dir, "browser_run accepts at most five additional artifacts", "invalid_action", "invalid_action")
	}
	response.Command = "node"
	response.Args = []string{runner, resolved, dir}
	response.Artifacts = append([]string{
		filepath.Join(dir, "playwright-trace.zip"),
		filepath.Join(dir, "browser-live.png"),
		filepath.Join(dir, "browser-pages.json"),
	}, response.Artifacts...)
	response.Summary = strings.TrimSpace(response.Summary)
	response.Impact = strings.TrimSpace(response.Impact)
	response.Target = strings.TrimSpace(response.Target)
	if response.Risk == "" {
		response.Risk = "unknown"
	}
	return response, nil
}
