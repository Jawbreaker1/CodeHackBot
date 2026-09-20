package guided

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavePreferencesAppliesSubscriptionInputBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "preferences.json")

	got, err := savePreferences(ctx, NewConsole(ctx, strings.NewReader(""), io.Discard), path, preferences{Provider: "subscription", Model: "gpt-daybreak-blue-latest"})
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxInputBytes != SubscriptionInputByteLimit {
		t.Fatalf("subscription input bytes = %d, want %d", got.MaxInputBytes, SubscriptionInputByteLimit)
	}
}
