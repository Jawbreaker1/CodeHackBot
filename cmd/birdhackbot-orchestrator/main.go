package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Jawbreaker1/CodeHackBot/internal/buildinfo"
	"github.com/Jawbreaker1/CodeHackBot/internal/guided"
	"github.com/Jawbreaker1/CodeHackBot/internal/reporoot"
)

func main() {
	version := flag.Bool("version", false, "print version")
	flag.Parse()

	if *version {
		fmt.Println(buildinfo.Version)
		return
	}

	root, err := reporoot.Find(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := (guided.App{RepoRoot: root, Reader: os.Stdin, Writer: os.Stdout}).Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "BirdHackBot:", err)
		if ctx.Err() != nil {
			os.Exit(130)
		}
		os.Exit(1)
	}
}
