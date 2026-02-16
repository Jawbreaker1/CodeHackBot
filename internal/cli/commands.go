package cli

import (
	"io"
	"strings"
)

func (r *Runner) handleCommand(line string) error {
	parts := strings.Fields(strings.TrimPrefix(line, "/"))
	if len(parts) == 0 {
		return nil
	}
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	if r.planWizardActive() && cmd != "plan" && cmd != "help" && cmd != "stop" && cmd != "exit" && cmd != "quit" {
		r.logger.Printf("Planning mode active. Use /plan done or /plan cancel.")
		return nil
	}

	switch cmd {
	case "help":
		r.printHelp()
	case "init":
		return r.handleInit(args)
	case "permissions":
		return r.handlePermissions(args)
	case "verbose":
		return r.handleVerbose(args)
	case "context":
		if len(args) > 0 && strings.ToLower(args[0]) == "show" {
			return r.handleContextShow()
		}
		r.logger.Printf("Context: max_recent=%d summarize_every=%d summarize_at=%d%%", r.cfg.Context.MaxRecentOutputs, r.cfg.Context.SummarizeEvery, r.cfg.Context.SummarizeAtPercent)
	case "ledger":
		return r.handleLedger(args)
	case "status":
		r.handleStatus()
	case "plan":
		return r.handlePlan(args)
	case "next":
		return r.handleNext(args)
	case "execute":
		return r.handleExecute(args)
	case "assist":
		return r.handleAssist(args)
	case "script":
		return r.handleScript(args)
	case "clean":
		return r.handleClean(args)
	case "ask":
		return r.handleAsk(strings.Join(args, " "))
	case "browse":
		return r.handleBrowse(args)
	case "crawl":
		return r.handleCrawl(args)
	case "links", "parse_links":
		return r.handleParseLinks(args)
	case "read", "read_file":
		return r.handleReadFile(args)
	case "ls", "list_dir":
		return r.handleListDir(args)
	case "write", "write_file":
		return r.handleWriteFile(args)
	case "summarize":
		return r.handleSummarize(args)
	case "run":
		return r.handleRun(args)
	case "report":
		return r.handleReport(args)
	case "msf":
		return r.handleMSF(args)
	case "resume":
		return r.handleResume()
	case "stop":
		r.Stop()
		return nil
	case "exit", "quit":
		r.Stop()
		return io.EOF
	default:
		r.logger.Printf("Unknown command: /%s", cmd)
	}
	return nil
}
