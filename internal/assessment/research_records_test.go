package assessment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/researchrecord"
)

func TestResearchRecordsKeepWorkerEvidenceAndIdentity(t *testing.T) {
	own := []ctxpacket.ExecutionResult{{LogRefs: []string{"build.log", "advisory.log"}}}
	software := []researchrecord.SoftwareObservation{{ID: "stack", Product: "Example Server", Version: "1.2.3", EvidenceKind: "package_inventory", Evidence: []string{"build.log"}}}
	advisories := []researchrecord.AdvisoryAssessment{{ID: "notice", AdvisoryID: "CVE-2026-1234", SoftwareID: "stack", Applicability: "affected", Reason: "Vendor package build predates the corrected release", Evidence: []string{"advisory.log"}}}
	gotSoftware, gotAdvisories, err := acceptResearchRecords("identify", nil, own, software, advisories)
	if err != nil || gotSoftware[0].ID != "identify:stack" || gotAdvisories[0].ID != "identify:notice" || gotAdvisories[0].SoftwareID != "identify:stack" {
		t.Fatalf("records were not linked: software=%+v advisory=%+v err=%v", gotSoftware, gotAdvisories, err)
	}
	advisories[0].Evidence = []string{"unregistered.log"}
	if _, _, err := acceptResearchRecords("identify", nil, own, software, advisories); err == nil {
		t.Fatal("accepted an advisory without a registered worker source")
	}
	advisories[0].Evidence = []string{"advisory.log"}
	advisories[0].SoftwareID = "missing"
	if _, _, err := acceptResearchRecords("identify", nil, own, software, advisories); err == nil {
		t.Fatal("accepted an advisory for an unobserved product")
	}
	prior := []Result{{Task: Task{ID: "identify"}, Status: "done", Software: gotSoftware}}
	verification := []researchrecord.AdvisoryAssessment{{ID: "notice", AdvisoryID: "VENDOR-BULLETIN-42", SoftwareID: "identify:stack", Applicability: "affected", Reason: "The observed build has no vendor backport", Evidence: []string{"advisory.log"}}}
	_, later, err := acceptResearchRecords("verify", prior, own, nil, verification)
	if err != nil || len(later) != 1 || later[0].SoftwareID != "identify:stack" {
		t.Fatalf("independent worker lost the prior software identity: later=%+v err=%v", later, err)
	}
	duplicated := []researchrecord.SoftwareObservation{{ID: "identify:stack", Product: "Example Server", Version: "1.2.3", EvidenceKind: "package_inventory", Evidence: []string{"build.log"}}}
	reused, later, err := acceptResearchRecords("verify", prior, own, duplicated, verification)
	if err != nil || len(reused) != 0 || len(later) != 1 || later[0].SoftwareID != "identify:stack" {
		t.Fatalf("duplicated dependency identity should remain a reference, not a new observation: software=%+v advisory=%+v err=%v", reused, later, err)
	}
	duplicated[0].Version = "9.9.9"
	if _, _, err := acceptResearchRecords("verify", prior, own, duplicated, verification); err == nil {
		t.Fatal("accepted a conflicting copy of the dependency identity")
	}
	newLocal := []researchrecord.SoftwareObservation{{ID: "verify:local", Product: "Example Server", Version: "1.2.3", EvidenceKind: "package_inventory", Evidence: []string{"build.log"}}}
	newAdvisory := []researchrecord.AdvisoryAssessment{{ID: "verify:notice", AdvisoryID: "VENDOR-BULLETIN-42", SoftwareID: "verify:local", Applicability: "affected", Reason: "Independent review", Evidence: []string{"advisory.log"}}}
	reused, later, err = acceptResearchRecords("verify", prior, own, newLocal, newAdvisory)
	if err != nil || len(reused) != 1 || reused[0].ID != "verify:local" || later[0].ID != "verify:notice" || later[0].SoftwareID != "verify:local" {
		t.Fatalf("task-prefixed local records were not normalized: software=%+v advisory=%+v err=%v", reused, later, err)
	}
	result := Result{Status: "done", Summary: "The independent challenge was supported", Verification: &VerificationResult{Verdict: "supported"}}
	noteResearchRecordError(&result, fmt.Errorf("invalid optional ID"))
	if result.Status != "done" || result.Verification.Verdict != "supported" || !strings.Contains(result.Summary, "records were not saved") {
		t.Fatalf("invalid optional metadata erased a valid challenge: %+v", result)
	}
	if handoff := strings.Join(workerHandoff(prior, []string{"identify"}, "/tasks"), "\n"); !strings.Contains(handoff, "identify:stack") {
		t.Fatalf("dependency handoff omitted structured software identity: %s", handoff)
	}
}

func exposureFixture() (State, Finding) {
	discovery := Result{Task: Task{ID: "identify"}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"build.log"}}}, Software: []researchrecord.SoftwareObservation{{ID: "identify:stack", Product: "Example Server", Version: "1.2.3", EvidenceKind: "package_inventory", Evidence: []string{"build.log"}}}}
	validation := Result{Task: Task{ID: "verify", Verification: &VerificationRequest{Claim: "The deployed build is affected", Alternative: "A vendor backport or a different instance removes exposure"}}, Status: "done", Evidence: []ctxpacket.ExecutionResult{{LogRefs: []string{"verify.log", "advisory.log"}}}, Verification: &VerificationResult{Verdict: "supported", AlternativeResult: "The package build and target instance match; no backport is present", Reason: "Vendor advisory identifies this build", Evidence: []string{"verify.log"}}, Advisories: []researchrecord.AdvisoryAssessment{{ID: "verify:notice", AdvisoryID: "CVE-2026-1234", SoftwareID: "identify:stack", Applicability: "affected", Reason: "The vendor build is within the affected range and has no backport", Evidence: []string{"advisory.log"}}}}
	state := State{Limits: DefaultLimits(), Plans: []Decision{{Tasks: []Task{discovery.Task}}, {Tasks: []Task{validation.Task}}}, Results: []Result{discovery, validation}}
	finding := Finding{Title: "Affected server build", Status: "confirmed_exposure", Severity: "high", Confidence: "high", CVEIDs: []string{"CVE-2026-1234"}, AdvisoryAssessmentID: "verify:notice", AffectedSoftware: []string{"Example Server 1.2.3"}, References: []string{"advisory.log"}, ValidationTask: "verify", Impact: "The installed build is affected; exploitation was not tested", Steps: []string{"Read installed build", "Check vendor advisory and backport status"}, Evidence: []string{"build.log", "verify.log"}, Remediation: []string{"Apply the vendor corrected build"}}
	return state, finding
}

func TestConfirmedExposureRequiresRecordedBuildAdvisoryAndIndependentCheck(t *testing.T) {
	state, finding := exposureFixture()
	decision := Decision{Summary: "The affected deployment was confirmed without exploitation", Complete: true, Findings: []Finding{finding}}
	if err := validateDecision(decision, state); err != nil {
		t.Fatalf("confirmed exposure rejected: %v", err)
	}
	vendorOnly := finding
	vendorOnly.CVEIDs = nil
	state.Results[1].Advisories[0].AdvisoryID = "VENDOR-BULLETIN-42"
	if err := validateDecision(Decision{Summary: "vendor bulletin", Complete: true, Findings: []Finding{vendorOnly}}, state); err != nil {
		t.Fatalf("vendor advisory without a CVE was rejected: %v", err)
	}
	state.Results[1].Advisories[0].AdvisoryID = "CVE-2026-1234"
	verified, candidates, _ := reportFindingGroups(State{Plans: []Decision{decision}, Results: state.Results})
	if len(verified) != 1 || len(candidates) != 0 {
		t.Fatalf("confirmed exposure was not reportable: verified=%d candidates=%d", len(verified), len(candidates))
	}
	cases := map[string]func(*State, *Finding){
		"patched or backported":      func(s *State, _ *Finding) { s.Results[1].Advisories[0].Applicability = "not_affected" },
		"unknown applicability":      func(s *State, _ *Finding) { s.Results[1].Advisories[0].Applicability = "unknown" },
		"banner only":                func(s *State, _ *Finding) { s.Results[0].Software[0].EvidenceKind = "banner" },
		"wrong advisory":             func(_ *State, f *Finding) { f.CVEIDs = []string{"CVE-2026-5678"} },
		"discarded optional record":  func(s *State, _ *Finding) { s.Results[1].Advisories = nil },
		"no advisory source":         func(_ *State, f *Finding) { f.References = nil },
		"no independent challenge":   func(s *State, _ *Finding) { s.Results[1].Verification = nil },
		"no matching build evidence": func(_ *State, f *Finding) { f.Evidence = []string{"verify.log"} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			current, claim := exposureFixture()
			change(&current, &claim)
			if err := validateDecision(Decision{Summary: "claim", Complete: true, Findings: []Finding{claim}}, current); err == nil {
				t.Fatal("unverified exposure was accepted")
			}
		})
	}
	wrongWorker := finding
	wrongWorker.AdvisoryAssessmentID = "identify:notice"
	state.Results[0].Advisories = []researchrecord.AdvisoryAssessment{{ID: "identify:notice", AdvisoryID: "CVE-2026-1234", SoftwareID: "identify:stack", Applicability: "affected", Reason: "Initial review", Evidence: []string{"build.log"}}}
	if err := validateDecision(Decision{Summary: "claim", Complete: true, Findings: []Finding{wrongWorker}}, state); err == nil || !strings.Contains(err.Error(), "validation task") {
		t.Fatalf("wrong advisory owner was not explained: %v", err)
	}
	state.Results[1].Advisories[0].Applicability = "not_affected"
	reviewed := DisplayFindings(State{Plans: []Decision{decision}, Results: state.Results})
	if len(reviewed) != 1 || reviewed[0].Status != "candidate" {
		t.Fatalf("historical exposure claim was not downgraded: %+v", reviewed)
	}
	if !strings.Contains(coordinatorPrompt(state), "confirmed_exposure") {
		t.Fatal("coordinator prompt cannot use the exposure distinction")
	}
}

func TestExposureReportsDescribeVerificationWithoutExploitClaim(t *testing.T) {
	state, finding := exposureFixture()
	state.ID = "synthetic-exposure"
	state.Plans = append(state.Plans, Decision{Complete: true, Summary: "The fixture package is affected; no exploit was attempted", Findings: []Finding{finding}})
	root := t.TempDir()
	if err := writeReport(root, state); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(filepath.Join(root, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	outputs := [][]byte{canonical}
	for _, format := range []ReportFormat{OWASPReport, PTESReport} {
		output, err := RenderFormattedReport(state, format)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, output)
	}
	for _, output := range outputs {
		text := string(output)
		if !strings.Contains(text, "CVE-2026-1234") || !strings.Contains(text, "Not tested or reproduced") && !strings.Contains(text, "not tested or reproduced") || !strings.Contains(text, "verify:notice") {
			t.Fatalf("report lost advisory or exploit distinction: %s", text)
		}
	}
}
