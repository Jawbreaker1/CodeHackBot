package main

import (
	"flag"
	"fmt"
	"log"

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
	)

	flag.BoolVar(&showVersion, "version", false, "Print version")
	flag.StringVar(&replayID, "replay", "", "Start replay session from sessions/<id>")
	flag.StringVar(&resumeID, "resume", "", "Resume session from sessions/<id>")
	flag.StringVar(&configPath, "config", "", "Path to default config JSON")
	flag.StringVar(&profileName, "profile", "", "Profile name under config/profiles/")
	flag.Parse()

	if showVersion {
		fmt.Printf("BirdHackBot %s\n", version)
		return
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

	log.Printf("Loaded config: %v", paths)
	if mode != "new" {
		log.Printf("Mode: %s (session %s)", mode, sessionID)
	}

	runner := cli.NewRunner(cfg, sessionID)
	if err := runner.Run(); err != nil {
		log.Fatalf("CLI exited: %v", err)
	}
}
