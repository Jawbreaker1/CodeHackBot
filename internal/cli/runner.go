package cli

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/llm"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

type Runner struct {
	cfg               config.Config
	sessionID         string
	logger            *log.Logger
	reader            *bufio.Reader
	defaultConfigPath string
	profilePath       string
	stopOnce          sync.Once
	currentTask       string
	currentTaskStart  time.Time
	currentMode       string
	planWizard        *planWizard
	history           []string
	historyIndex      int
	historyScratch    string
	llmMu             sync.Mutex
	llmInFlight       bool
	llmLabel          string
	llmStarted        time.Time
	pendingAssistGoal string

	lastAssistCmdKey   string
	lastAssistCmdSeen  int
	lastAssistQuestion string
	lastAssistQSeen    int

	lastBrowseLogPath  string
	lastBrowseBodyPath string
	lastBrowseURL      string
	lastActionLogPath  string
	lastKnownTarget    string

	inputRenderLines int

	llmGuard llm.Guard
}

func NewRunner(cfg config.Config, sessionID, defaultConfigPath, profilePath string) *Runner {
	return &Runner{
		cfg:               cfg,
		sessionID:         sessionID,
		logger:            log.New(os.Stdout, fmt.Sprintf("[session:%s] ", sessionID), 0),
		reader:            bufio.NewReader(os.Stdin),
		defaultConfigPath: defaultConfigPath,
		profilePath:       profilePath,
		historyIndex:      -1,
		llmGuard:          llm.NewGuard(cfg.LLM.MaxFailures, time.Duration(cfg.LLM.CooldownSeconds)*time.Second),
	}
}

func (r *Runner) Run() error {
	r.printLogo()
	r.logger.Printf("BirdHackBot interactive mode. Type /help for commands.")
	for {
		line, err := r.readLine(r.prompt())
		if err != nil && err != io.EOF {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if err == io.EOF {
				r.Stop()
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, "/") {
			if err := r.handleCommand(line); err != nil {
				r.logger.Printf("Command error: %v", err)
			}
			if err == io.EOF {
				r.Stop()
				return nil
			}
			continue
		}
		if r.pendingAssistGoal != "" {
			if err := r.handleAssistFollowUp(line); err != nil {
				r.logger.Printf("Assist follow-up error: %v", err)
			}
			if err == io.EOF {
				r.Stop()
				return nil
			}
			continue
		}
		if r.planWizardActive() {
			if err := r.handlePlanWizardInput(line); err != nil {
				r.logger.Printf("Plan error: %v", err)
			}
			if err == io.EOF {
				r.Stop()
				return nil
			}
			continue
		}
		r.appendConversation("User", line)
		if assistErr := r.handleAssistGoal(line, false); assistErr != nil {
			r.logger.Printf("Assist error: %v", assistErr)
		}
		if err == io.EOF {
			r.Stop()
			return nil
		}
	}
}

func (r *Runner) ensureSessionScaffold() (string, error) {
	sessionDir := filepath.Join(r.cfg.Session.LogDir, r.sessionID)
	if _, err := os.Stat(sessionDir); err == nil {
		return sessionDir, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat session dir: %w", err)
	}
	if _, err := session.CreateScaffold(session.ScaffoldOptions{
		RootDir:           r.cfg.Session.LogDir,
		SessionID:         r.sessionID,
		PlanFilename:      r.cfg.Session.PlanFilename,
		InventoryFilename: r.cfg.Session.InventoryFilename,
		LedgerFilename:    r.cfg.Session.LedgerFilename,
		CreateLedger:      r.cfg.Session.LedgerEnabled,
	}); err != nil {
		return "", err
	}
	sessionConfigPath := config.SessionPath(r.cfg.Session.LogDir, r.sessionID)
	if _, err := os.Stat(sessionConfigPath); os.IsNotExist(err) {
		if err := config.Save(sessionConfigPath, r.cfg); err != nil {
			return "", fmt.Errorf("save session config: %w", err)
		}
	}
	return sessionDir, nil
}

func (r *Runner) Stop() {
	r.stopOnce.Do(func() {
		if err := r.handleStop(); err != nil {
			r.logger.Printf("Session stop error: %v", err)
		}
	})
}

func (r *Runner) printHelp() {
	r.logger.Printf("Commands: /init /permissions /verbose /context [/show] /ledger /status /plan /next /execute /assist /script /clean /ask /browse /crawl /links /read /ls /write /summarize /run /msf /report /resume /stop /exit")
	r.logger.Printf("Example: /permissions readonly")
	r.logger.Printf("Plain text routes to /assist. Use /ask for explicit non-agentic chat.")
	r.logger.Printf("/plan starts guided planning; /plan done or /plan cancel ends it.")
	r.logger.Printf("Session logs live under: %s", filepath.Clean(r.cfg.Session.LogDir))
}

func (r *Runner) printLogo() {
	data, err := os.ReadFile(filepath.Join("assets", "logo.ascii"))
	if err != nil {
		return
	}
	fmt.Println(string(data))
}
