package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Jawbreaker1/CodeHackBot/internal/cli"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

const version = "0.0.0-dev"

func main() {
	var (
		showVersion bool
		replayID    string
		resumeID    string
		configPath  string
		profileName string
		permLevel   string
		ledgerMode  string
	)

	flag.BoolVar(&showVersion, "version", false, "Print version")
	flag.StringVar(&replayID, "replay", "", "Start replay session from sessions/<id>")
	flag.StringVar(&resumeID, "resume", "", "Resume session from sessions/<id>")
	flag.StringVar(&configPath, "config", "", "Path to default config JSON")
	flag.StringVar(&profileName, "profile", "", "Profile name under config/profiles/")
	flag.StringVar(&permLevel, "permissions", "", "Override permissions: readonly, default, all")
	flag.StringVar(&ledgerMode, "ledger", "", "Override ledger: on or off")
	flag.Parse()

	if showVersion {
		fmt.Printf("BirdHackBot %s\n", version)
		return
	}

	if replayID != "" && resumeID != "" {
		log.Fatal("Use either --replay or --resume, not both")
	}

	mode := "new"
	sessionID := session.NewID()
	switch {
	case replayID != "":
		mode = "replay"
		sessionID = replayID
	case resumeID != "":
		mode = "resume"
		sessionID = resumeID
	}

	profilePath := ""
	if profileName != "" {
		profilePath = config.ProfilePath(profileName)
	}

	cfg, paths, err := config.Load(configPath, profilePath, "")
	if err != nil {
		log.Fatalf("Config load failed: %v", err)
	}

	sessionConfigPath := ""
	if mode == "replay" || mode == "resume" {
		sessionConfigPath = config.SessionPath(cfg.Session.LogDir, sessionID)
	}
	if sessionConfigPath != "" {
		cfg, paths, err = config.Load(configPath, profilePath, sessionConfigPath)
		if err != nil {
			log.Fatalf("Config load failed: %v", err)
		}
	}

	log.SetPrefix(fmt.Sprintf("[session:%s] ", sessionID))
	log.SetFlags(log.LstdFlags)

	if permLevel != "" {
		level := permLevel
		switch level {
		case "readonly", "default", "all":
			cfg.Permissions.Level = level
			cfg.Permissions.RequireApproval = level == "default"
		default:
			log.Fatalf("Invalid --permissions value: %s", level)
		}
	}

	if ledgerMode != "" {
		switch ledgerMode {
		case "on":
			cfg.Session.LedgerEnabled = true
		case "off":
			cfg.Session.LedgerEnabled = false
		default:
			log.Fatalf("Invalid --ledger value: %s", ledgerMode)
		}
	}

	log.Printf("Loaded config: %v", paths)
	if mode != "new" {
		log.Printf("Mode: %s (session %s)", mode, sessionID)
	}

	defaultPath := configPath
	if defaultPath == "" {
		defaultPath = config.DefaultPath()
	}

	runner := cli.NewRunner(cfg, sessionID, defaultPath, profilePath)
	defer runner.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal: %s", sig.String())
		runner.Stop()
		os.Exit(1)
	}()

	if err := runner.Run(); err != nil {
		log.Fatalf("CLI exited: %v", err)
	}
}
