package subscription

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/localauth"
)

// LocalBridge owns a loopback subscription adapter and its private client
// token. Codex credentials remain in Codex's store and are read on each call.
type LocalBridge struct {
	BaseURL   string
	TokenFile string
	close     func()
}

func (b *LocalBridge) Close() {
	if b != nil && b.close != nil {
		b.close()
		b.close = nil
	}
}

func StartLocalBridge(ctx context.Context, codexHome string) (*LocalBridge, error) {
	dir, err := os.MkdirTemp("", "birdhackbot-provider-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	tokenPath := filepath.Join(dir, "client-token")
	if err := localauth.Create(tokenPath); err != nil {
		cleanup()
		return nil, err
	}
	token, err := localauth.Read(tokenPath)
	if err != nil {
		cleanup()
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cleanup()
		return nil, err
	}
	server := &http.Server{
		Handler:           Handler(&Provider{Auth: &CodexAuth{Home: codexHome}}, token),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      190 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() { _ = server.Serve(listener) }()
	return &LocalBridge{
		BaseURL:   "http://" + listener.Addr().String() + "/v1",
		TokenFile: tokenPath,
		close: func() {
			_ = server.Close()
			cleanup()
		},
	}, nil
}
