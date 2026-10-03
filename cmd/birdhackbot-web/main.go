package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/buildinfo"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/reporoot"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
	"github.com/Jawbreaker1/CodeHackBot/internal/webapp"
)

func main() {
	version := flag.Bool("version", false, "print version")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	repo := flag.String("repo-root", "", "repository root; defaults to the current checkout")
	sessions := flag.String("sessions-dir", "", "directory for web assessment sessions")
	baseURL := flag.String("llm-base-url", "", "OpenAI-compatible base URL without trailing /chat/completions")
	model := flag.String("llm-model", "", "LLM model ID")
	tokenFile := flag.String("llm-token-file", "", "optional local subscription bridge token file")
	reasoning := flag.String("reasoning-effort", "", "provider reasoning effort, such as low")
	maxOutput := flag.Int("max-output-tokens", 32768, "maximum output tokens per local-model request")
	maxInput := flag.Int("max-input-bytes", 0, "maximum combined model input bytes (profile default when omitted)")
	profilesFile := flag.String("model-profiles-file", "", "model profile JSON file; defaults to config/model-profiles.local.json when present")
	flag.Parse()
	if *version {
		fmt.Println(buildinfo.Version)
		return
	}

	root := *repo
	var err error
	if strings.TrimSpace(root) == "" {
		root, err = reporoot.Find(".")
		if err != nil {
			fatal(err)
		}
	}
	researchMode := strings.TrimSpace(os.Getenv("BIRDHACKBOT_RESEARCH_MODE"))
	if researchMode == "" {
		researchMode = "connected"
	}
	if researchMode != "connected" && researchMode != "air_gapped" && researchMode != "offline" {
		fatal(fmt.Errorf("BIRDHACKBOT_RESEARCH_MODE must be connected or air_gapped"))
	}
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{"approval_mode": "operator_selected_session_policy", "research_mode": researchMode})
	if err != nil {
		fatal(err)
	}
	requestReasoning, requestMaxOutput := *reasoning, *maxOutput
	inputLimit := *maxInput
	// The local subscription bridge accepts reasoning effort; its output limit
	// remains the provider-backed subscription budget.
	if strings.TrimSpace(*tokenFile) != "" {
		requestMaxOutput = llmclient.SubscriptionMaxOutputTokens
		if inputLimit == 0 {
			inputLimit = llmclient.SubscriptionInputByteLimit
		}
	} else if inputLimit == 0 {
		inputLimit = llmclient.DefaultInputByteLimit
	}
	client := llmclient.Client{BaseURL: *baseURL, Model: *model, AuthTokenFile: *tokenFile, ReasoningEffort: requestReasoning, MaxOutputTokens: requestMaxOutput, MaxInputBytes: inputLimit}
	stopContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	profilesPath := *profilesFile
	if profilesPath == "" {
		candidate := filepath.Join(root, "config", "model-profiles.local.json")
		if _, err := os.Stat(candidate); err == nil {
			profilesPath = candidate
		}
	}
	config := webapp.Config{RepoRoot: root, SessionsRoot: *sessions, LLM: client, Frame: frame, Limits: assessment.DefaultLimits()}
	if profilesPath != "" {
		profiles, err := webapp.LoadModelProfiles(profilesPath)
		if err != nil {
			fatal(fmt.Errorf("load model profiles %s: %w", profilesPath, err))
		}
		config.Profiles, config.DefaultProfile = profiles.Profiles, profiles.Default
	}
	closeBridge, err := attachManagedBridge(stopContext, config.Profiles)
	if err != nil {
		fatal(fmt.Errorf("start managed subscription bridge: %w", err))
	}
	defer closeBridge()
	server := webapp.NewServer(config)
	// Conversation requests may wait for inference or human approval. A short
	// write deadline can discard a completed response and cause a POST retry.
	// Model requests and shutdown retain their own cancellation limits.
	httpServer := &http.Server{Addr: *addr, Handler: server, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-stopContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownContext)
	}()
	fmt.Printf("BirdHackBot web UI: http://%s\n", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		closeBridge()
		fatal(err)
	}
}

func attachManagedBridge(ctx context.Context, profiles []webapp.ModelProfile) (func(), error) {
	needed := false
	for _, profile := range profiles {
		needed = needed || profile.ManagedBridge
	}
	if !needed {
		return func() {}, nil
	}
	codexDir := os.Getenv("CODEX_HOME")
	if codexDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		codexDir = filepath.Join(home, ".codex")
	}
	bridge, err := subscription.StartLocalBridge(ctx, codexDir)
	if err != nil {
		return nil, err
	}
	for i := range profiles {
		if profiles[i].ManagedBridge {
			profiles[i].BaseURL = bridge.BaseURL
			profiles[i].TokenFile = bridge.TokenFile
		}
	}
	return bridge.Close, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "birdhackbot-web:", err)
	os.Exit(1)
}
