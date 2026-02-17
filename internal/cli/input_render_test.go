package cli

import "testing"

func TestVisualLineCount(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  int
	}{
		{name: "empty", text: "", width: 80, want: 1},
		{name: "single line short", text: "abc", width: 80, want: 1},
		{name: "exact width", text: "1234567890", width: 10, want: 1},
		{name: "wrap once", text: "12345678901", width: 10, want: 2},
		{name: "bad width fallback", text: "12345678901", width: 0, want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := visualLineCount(tc.text, tc.width)
			if got != tc.want {
				t.Fatalf("visualLineCount(%q,%d) = %d; want %d", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

func TestFitSingleLine(t *testing.T) {
	got := fitSingleLine("BirdHackBot> this is a long command", 16)
	if got == "" {
		t.Fatalf("expected non-empty fitted line")
	}
	if len([]rune(got)) > maxDisplayColumns(16) {
		t.Fatalf("fitted line too long: %q", got)
	}
	if got[:3] != "..." {
		t.Fatalf("expected ellipsis prefix, got %q", got)
	}
}

func TestPadOrTrimSingleLine(t *testing.T) {
	got := padOrTrimSingleLine("ctx:10%", 20)
	if len([]rune(got)) != maxDisplayColumns(20) {
		t.Fatalf("unexpected padded width: %d", len([]rune(got)))
	}
	trimmed := padOrTrimSingleLine("this is an extremely long status line", 12)
	if len([]rune(trimmed)) > maxDisplayColumns(12) {
		t.Fatalf("trimmed line exceeds width: %q", trimmed)
	}
}
