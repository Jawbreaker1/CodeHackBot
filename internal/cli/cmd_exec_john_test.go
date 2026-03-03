package cli

import "testing"

func TestDetectJohnHashPathDirectCommand(t *testing.T) {
	path, ok := detectJohnHashPath("john", []string{"--wordlist=/usr/share/wordlists/rockyou.txt", "secret.zip.hash"}, "No password hashes left to crack")
	if !ok || path != "secret.zip.hash" {
		t.Fatalf("unexpected detect result: ok=%v path=%q", ok, path)
	}
}

func TestDetectJohnHashPathFromShellScript(t *testing.T) {
	path, ok := detectJohnHashPath(
		"bash",
		[]string{"-c", "zip2john secret.zip > secret.zip.hash && john --wordlist=/usr/share/wordlists/rockyou.txt secret.zip.hash"},
		"Loaded 1 password hash\nNo password hashes left to crack (see FAQ)",
	)
	if !ok || path != "secret.zip.hash" {
		t.Fatalf("unexpected detect result: ok=%v path=%q", ok, path)
	}
}

func TestDetectJohnHashPathRequiresSignalPhrase(t *testing.T) {
	if path, ok := detectJohnHashPath("john", []string{"secret.zip.hash"}, "Loaded 1 password hash"); ok || path != "" {
		t.Fatalf("expected detection disabled without john completion phrase, got ok=%v path=%q", ok, path)
	}
}
