package localauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateTokenCannotBeOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := Create(path); err != nil {
		t.Fatal(err)
	}
	before, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Create(path); err == nil {
		t.Fatal("overwrote existing token")
	}
	after, err := Read(path)
	if err != nil || after != before {
		t.Fatal("token changed")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("accepted public credential file")
	}
}
