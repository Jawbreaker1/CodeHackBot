package msf

import (
	"strings"
	"testing"
)

func TestBuildSearch(t *testing.T) {
	q := Query{Service: "http", Platform: "linux", Keyword: "apache"}
	search := BuildSearch(q)
	if !strings.Contains(search, "service:http") || !strings.Contains(search, "platform:linux") || !strings.Contains(search, "apache") {
		t.Fatalf("unexpected search: %s", search)
	}
}

func TestParseSearchOutput(t *testing.T) {
	sample := `Matching Modules
================

   exploit/linux/http/test_module      2020-01-01       excellent  Test module
   auxiliary/scanner/http/test_scan    2021-01-01       normal     Test scanner
`
	lines := ParseSearchOutput(sample)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "exploit/") {
		t.Fatalf("unexpected line: %s", lines[0])
	}
}
