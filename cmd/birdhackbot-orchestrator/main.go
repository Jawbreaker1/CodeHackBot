package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "dev"

func main() {
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("birdhackbot-orchestrator %s\n", version)
		return
	}

	fmt.Fprintln(os.Stdout, "birdhackbot-orchestrator scaffold ready")
	fmt.Fprintln(os.Stdout, "planned commands: start, status, workers, events, stop")
}
