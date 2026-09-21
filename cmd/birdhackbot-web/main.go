package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/buildinfo"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/reporoot"
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
	frame, err := behavior.Load(root, "assessment_coordinator", map[string]string{"approval_mode": "per_action"})
	if err != nil {
		fatal(err)
	}
	requestReasoning, requestMaxOutput := *reasoning, *maxOutput
	inputLimit := *maxInput
	// The local subscription bridge deliberately exposes only its text contract;
	// provider-specific reasoning and max-token fields are for local servers.
	if strings.TrimSpace(*tokenFile) != "" {
		requestReasoning, requestMaxOutput = "", llmclient.SubscriptionMaxOutputTokens
		if inputLimit == 0 {
			inputLimit = llmclient.SubscriptionInputByteLimit
		}
	} else if inputLimit == 0 {
		inputLimit = llmclient.DefaultInputByteLimit
	}
	client := llmclient.Client{BaseURL: *baseURL, Model: *model, AuthTokenFile: *tokenFile, ReasoningEffort: requestReasoning, MaxOutputTokens: requestMaxOutput, MaxInputBytes: inputLimit}
	server := webapp.NewServer(webapp.Config{RepoRoot: root, SessionsRoot: *sessions, LLM: client, Frame: frame, Limits: assessment.DefaultLimits()})
	httpServer := &http.Server{Addr: *addr, Handler: server, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stopContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-stopContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownContext)
	}()
	fmt.Printf("BirdHackBot web UI: http://%s\n", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "birdhackbot-web:", err)
	os.Exit(1)
}
