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
