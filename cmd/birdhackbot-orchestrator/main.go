package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Jawbreaker1/CodeHackBot/internal/buildinfo"
)

func main() {
	version := flag.Bool("version", false, "print version")
	flag.Parse()

	if *version {
		fmt.Println(buildinfo.Version)
		return
	}

	fmt.Fprintln(os.Stderr, "birdhackbot-orchestrator rebuild: orchestrator not implemented yet")
}
