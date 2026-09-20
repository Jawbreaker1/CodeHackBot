package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
	"github.com/Jawbreaker1/CodeHackBot/internal/subscription"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "subscription bridge:", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "127.0.0.1:8787", "literal loopback IP and port")
	home := flag.String("codex-home", "", "Codex file credential directory (default CODEX_HOME or ~/.codex)")
	tokenPath := flag.String("token-file", "", "private local client token file, outside the repository")
	initToken := flag.Bool("init-token", false, "create the client token file and exit")
	flag.Parse()
	if *tokenPath == "" {
		return fmt.Errorf("--token-file is required")
	}
	if *initToken {
		return localauth.Create(*tokenPath)
	}
	token, err := localauth.Read(*tokenPath)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || !localauth.LoopbackHost(host) {
		return fmt.Errorf("--listen must use a literal loopback IP")
	}
	if *home == "" {
		*home = os.Getenv("CODEX_HOME")
	}
	if *home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*home = filepath.Join(userHome, ".codex")
	}
	*home, err = filepath.Abs(*home)
	if err != nil {
		return err
	}
	auth := &subscription.CodexAuth{Home: *home}
	if _, err := auth.Read(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           subscription.Handler(&subscription.Provider{Auth: auth}, token),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 190 * time.Second, IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("subscription bridge: http://%s/v1 (ChatGPT sign-in; no API-key fallback)\n", listener.Addr())
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
