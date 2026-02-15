package cli

import (
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
)

func TestShouldRouteToAssist(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-route", "", "")

	cases := []struct {
		in   string
		want bool
	}{
		{"who are you?", false},
		{"hello", false},
		{"scan 10.0.0.5", true},
		{"list files in this folder", true},
		{"what is inside README.md?", true},
		{"investigate systemverification.com", true},
		{"what is systemverification.com about?", true}, // URL hint -> assist (will browse/crawl)
	}
	for _, c := range cases {
		got := r.shouldRouteToAssist(c.in)
		if got != c.want {
			t.Fatalf("route(%q)=%v want %v", c.in, got, c.want)
		}
	}
}
