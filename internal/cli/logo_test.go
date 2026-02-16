package cli

import (
	"strings"
	"testing"
)

func TestColorizeLogoDisabled(t *testing.T) {
	logo := "bird\n\nBirdHackBot\n"
	got := colorizeLogo(logo, false)
	if got != logo {
		t.Fatalf("expected unchanged logo when disabled")
	}
}

func TestColorizeLogoEnabled(t *testing.T) {
	logo := "  .\n  -#-\n\nBirdHackBot\n"
	got := colorizeLogo(logo, true)
	if !strings.Contains(got, logoANSIBlueA) {
		t.Fatalf("expected bird line color in logo: %q", got)
	}
	if !strings.Contains(got, logoANSIName+"BirdHackBot"+logoANSIReset) {
		t.Fatalf("expected colored BirdHackBot label: %q", got)
	}
}
