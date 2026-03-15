package cli

import (
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/playbook"
)

func TestBuildPlaybookHintsRecoverModeUsesSingleCompactHint(t *testing.T) {
	entries := []playbook.Entry{
		{
			Name:     "network-scan.md",
			Title:    "Network Scan",
			Content:  "## Goal\n- Identify reachable services safely.\n## Guardrails\n- Non-intrusive only.\n- No exploitation.\n## Procedure\n1. Baseline host discovery.\n- Example: `nmap -sn <cidr>`\n2. Targeted service scan.\n- Example: `nmap -sV --top-ports 100 <host>`\n",
			Keywords: []string{"network", "scan", "router", "nmap", "services"},
		},
		{
			Name:     "web-enum.md",
			Title:    "Web Enum",
			Content:  "## Goal\n- Enumerate web endpoints.\n## Guardrails\n- Passive first.\n",
			Keywords: []string{"web", "http", "headers"},
		},
	}
	goal := "Continue non-intrusive discovery of router 192.168.50.1 services"
	recent := "[time] run: nmap -sn 192.168.50.1\nout: host is up"

	hints := buildPlaybookHints(entries, goal, recent, "recover", 2, 60)
	if hints == "" {
		t.Fatalf("expected hints")
	}
	if !strings.Contains(hints, "network-scan.md") {
		t.Fatalf("expected network hint, got:\n%s", hints)
	}
	if strings.Contains(hints, "web-enum.md") {
		t.Fatalf("expected single hint in recover mode, got:\n%s", hints)
	}
	if len(hints) > playbookHintRecoverCharBudget+20 {
		t.Fatalf("expected compact recover hint, len=%d", len(hints))
	}
	if strings.Contains(hints, "## ") {
		t.Fatalf("expected compacted prose, got raw markdown:\n%s", hints)
	}
	if strings.Contains(hints, "nmap -sV") || strings.Contains(hints, "nmap -sn") {
		t.Fatalf("expected no step-by-step command recipe in hints, got:\n%s", hints)
	}
	if !strings.Contains(hints, "Tools: nmap") {
		t.Fatalf("expected tooling hint, got:\n%s", hints)
	}
}

func TestBuildPlaybookHintsReturnsEmptyWhenNoRelevantMatch(t *testing.T) {
	entries := []playbook.Entry{
		{
			Name:     "network-scan.md",
			Title:    "Network Scan",
			Content:  "## Goal\n- Identify services.\n",
			Keywords: []string{"network", "scan", "nmap", "ports"},
		},
	}
	goal := "Draft an OWASP report from existing evidence artifacts"

	hints := buildPlaybookHints(entries, goal, "", "execute-step", 2, 60)
	if hints != "" {
		t.Fatalf("expected no hints for unrelated goal, got:\n%s", hints)
	}
}

func TestBuildPlaybookHintsListsPlaybooksOnExplicitRequest(t *testing.T) {
	entries := []playbook.Entry{
		{Name: "network-scan.md", Title: "Network Scan"},
		{Name: "zip-attack.md", Title: "ZIP Attack"},
	}
	hints := buildPlaybookHints(entries, "show available playbooks", "", "", 2, 60)
	if !strings.Contains(hints, "network-scan.md") || !strings.Contains(hints, "zip-attack.md") {
		t.Fatalf("expected playbook list, got:\n%s", hints)
	}
}

func TestBuildPlaybookHintsCanUseRecentSignalForMatching(t *testing.T) {
	entries := []playbook.Entry{
		{
			Name:     "network-scan.md",
			Title:    "Network Scan",
			Content:  "## Guardrails\n- Non-intrusive only.\n",
			Keywords: []string{"network", "scan", "nmap"},
		},
	}
	goal := "Continue troubleshooting from latest evidence"
	recent := "out: Starting Nmap 7.98\nout: PORT STATE SERVICE"

	hints := buildPlaybookHints(entries, goal, recent, "recover", 1, 60)
	if hints == "" {
		t.Fatalf("expected match from recent evidence signal")
	}
	if !strings.Contains(hints, "network-scan.md") {
		t.Fatalf("expected network playbook hint, got:\n%s", hints)
	}
}

func TestBuildPlaybookHintsExpandsDetailWithLargerPlaybookLines(t *testing.T) {
	entries := []playbook.Entry{
		{
			Name:     "network-scan.md",
			Title:    "Network Scan",
			Content:  "## Goal\n- Identify reachable services safely.\n## Planning checklist\n- Confirm target attribution confidence before host-focused scans.\n- Record uncertainty and run one disambiguation action when identity is ambiguous.\n## Procedure\n- Compare hostnames and MAC vendor signals before target selection.\n- Prefer non-intrusive host discovery first.\n## Guardrails\n- Stay in approved scope.\n- Avoid auth attempts.\n- Keep scan rate bounded.\n",
			Keywords: []string{"network", "scan", "iphone", "target"},
		},
	}
	goal := "identify iphone target confidence before scanning"
	compact := buildPlaybookHints(entries, goal, "", "recover", 1, 60)
	expanded := buildPlaybookHints(entries, goal, "", "recover", 1, 180)
	if compact == "" || expanded == "" {
		t.Fatalf("expected both compact and expanded hints")
	}
	if len(expanded) <= len(compact) {
		t.Fatalf("expected expanded hint to be longer: compact=%d expanded=%d", len(compact), len(expanded))
	}
	if len(expanded) <= playbookHintRecoverCharBudget {
		t.Fatalf("expected expanded budget above default recover budget, len=%d", len(expanded))
	}
}

func TestAdaptivePlaybookHintCharBudgetDefaultAndScaled(t *testing.T) {
	if got := adaptivePlaybookHintCharBudget("recover", 60); got != playbookHintRecoverCharBudget {
		t.Fatalf("expected default recover budget %d, got %d", playbookHintRecoverCharBudget, got)
	}
	scaled := adaptivePlaybookHintCharBudget("recover", 180)
	if scaled <= playbookHintRecoverCharBudget {
		t.Fatalf("expected scaled budget above default, got %d", scaled)
	}
}
