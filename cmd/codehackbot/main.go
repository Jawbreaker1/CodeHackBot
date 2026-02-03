package main

import (
	"flag"
	"fmt"
)

const version = "0.0.0-dev"

func main() {
	var (
		showVersion bool
		replayID    string
		resumeID    string
	)

	flag.BoolVar(&showVersion, "version", false, "Print version")
	flag.StringVar(&replayID, "replay", "", "Start replay session from sessions/<id>")
	flag.StringVar(&resumeID, "resume", "", "Resume session from sessions/<id>")
	flag.Parse()

	switch {
	case showVersion:
		fmt.Printf("CodeHackBot %s\n", version)
	case replayID != "":
		fmt.Printf("Replay requested for session %s (stub)\n", replayID)
	case resumeID != "":
		fmt.Printf("Resume requested for session %s (stub)\n", resumeID)
	default:
		fmt.Println("CodeHackBot CLI (stub). Use --help for options.")
	}
}
