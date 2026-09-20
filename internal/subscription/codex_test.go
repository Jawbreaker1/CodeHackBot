package subscription

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCodexRefreshOnlyUsesAccountProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell protocol fixture")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
IFS= read -r init
printf '%s\n' "$init" > protocol.jsonl
printf '%s\n' '{"id":0,"result":{}}'
IFS= read -r initialized
printf '%s\n' "$initialized" >> protocol.jsonl
IFS= read -r account
printf '%s\n' "$account" >> protocol.jsonl
printf '%s\n' '{"id":1,"result":{"account":{"type":"chatgpt"}}}'
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := refreshWithCodex(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "protocol.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Exact protocol also proves no thread or turn was started by refresh.
	want := "{\"id\":0,\"method\":\"initialize\",\"params\":{\"clientInfo\":{\"name\":\"birdhackbot\",\"version\":\"0.1.0\"}}}\n{\"method\":\"initialized\"}\n{\"id\":1,\"method\":\"account/read\",\"params\":{\"refreshToken\":true}}\n"
	if string(b) != want {
		t.Fatalf("unexpected auth protocol: %s", b)
	}
}
