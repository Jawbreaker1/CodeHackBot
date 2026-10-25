package assessment

import (
	"fmt"
	"slices"
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/researchrecord"
)

// acceptResearchRecords keeps model-authored research attached to the actual
// command or artifact that produced it. It does not interpret advisory content.
func acceptResearchRecords(taskID string, prior []Result, own []ctxpacket.ExecutionResult, software []researchrecord.SoftwareObservation, advisories []researchrecord.AdvisoryAssessment) ([]researchrecord.SoftwareObservation, []researchrecord.AdvisoryAssessment, error) {
	if len(software) > 4 || len(advisories) > 8 {
		return nil, nil, fmt.Errorf("research result exceeds record limit")
	}
	refs := map[string]bool{}
	for _, execution := range own {
		for _, ref := range append(append([]string(nil), execution.LogRefs...), execution.ArtifactRefs...) {
			refs[ref] = true
		}
	}
	known := map[string]researchrecord.SoftwareObservation{}
	for _, result := range prior {
		if result.Status != "done" {
			continue
		}
		for _, observation := range result.Software {
			known[observation.ID] = observation
		}
	}
	local := map[string]string{}
	acceptedSoftware := make([]researchrecord.SoftwareObservation, 0, len(software))
	for i := range software {
		item := &software[i]
		if earlier, ok := known[item.ID]; ok {
			// A later worker may cite the earlier observation in both arrays.
			// Keep the original provenance; its new advisory must still cite
			// this worker's own execution evidence.
			if item.Product != earlier.Product || item.Component != earlier.Component || item.Version != earlier.Version {
				return nil, nil, fmt.Errorf("software observation %q conflicts with the earlier identity", item.ID)
			}
			continue
		}
		item.ID = strings.TrimPrefix(item.ID, taskID+":")
		if err := localRecordID(item.ID); err != nil {
			return nil, nil, fmt.Errorf("software observation: %w", err)
		}
		if local[item.ID] != "" || strings.TrimSpace(item.Product) == "" || len(item.Product) > 160 || len(item.Component) > 160 || len(item.Version) > 120 {
			return nil, nil, fmt.Errorf("software observation %q has duplicate ID or invalid identity", item.ID)
		}
		switch item.EvidenceKind {
		case "package_inventory", "build_metadata", "vendor_attestation", "banner", "source", "inference":
		default:
			return nil, nil, fmt.Errorf("software observation %q has unknown evidence kind", item.ID)
		}
		if err := ownReferences(item.Evidence, refs); err != nil {
			return nil, nil, fmt.Errorf("software observation %q: %w", item.ID, err)
		}
		fullID := taskID + ":" + item.ID
		local[item.ID] = fullID
		item.ID = fullID
		known[fullID] = *item
		acceptedSoftware = append(acceptedSoftware, *item)
	}
	seenAdvisories := map[string]bool{}
	for i := range advisories {
		item := &advisories[i]
		item.ID = strings.TrimPrefix(item.ID, taskID+":")
		if err := localRecordID(item.ID); err != nil {
			return nil, nil, fmt.Errorf("advisory assessment: %w", err)
		}
		if seenAdvisories[item.ID] || strings.TrimSpace(item.AdvisoryID) == "" || len(item.AdvisoryID) > 100 || strings.TrimSpace(item.Reason) == "" || len(item.Reason) > 800 {
			return nil, nil, fmt.Errorf("advisory assessment %q has duplicate ID or missing explanation", item.ID)
		}
		seenAdvisories[item.ID] = true
		switch item.Applicability {
		case "affected", "not_affected", "unknown":
		default:
			return nil, nil, fmt.Errorf("advisory assessment %q has unknown applicability", item.ID)
		}
		if full := local[item.SoftwareID]; full != "" {
			item.SoftwareID = full
		}
		if _, ok := known[item.SoftwareID]; !ok {
			return nil, nil, fmt.Errorf("advisory assessment %q refers to unknown software observation", item.ID)
		}
		if err := ownReferences(item.Evidence, refs); err != nil {
			return nil, nil, fmt.Errorf("advisory assessment %q: %w", item.ID, err)
		}
		item.ID = taskID + ":" + item.ID
	}
	return acceptedSoftware, advisories, nil
}

func localRecordID(id string) error {
	if id == "" || len(id) > 60 || strings.TrimSpace(id) != id || strings.ContainsAny(id, ":/\\\n\r\t ") {
		return fmt.Errorf("record ID must be a short task-local token")
	}
	return nil
}

func ownReferences(values []string, refs map[string]bool) error {
	if len(values) == 0 || len(values) > 6 {
		return fmt.Errorf("needs one to six registered execution references")
	}
	for _, ref := range values {
		if !refs[ref] {
			return fmt.Errorf("unregistered execution reference: %s", ref)
		}
	}
	return nil
}

func recordedResearch(state State) (map[string]researchrecord.SoftwareObservation, map[string]researchrecord.AdvisoryAssessment) {
	software := map[string]researchrecord.SoftwareObservation{}
	advisories := map[string]researchrecord.AdvisoryAssessment{}
	for _, result := range state.Results {
		if result.Status != "done" {
			continue
		}
		for _, item := range result.Software {
			software[item.ID] = item
		}
		for _, item := range result.Advisories {
			advisories[item.ID] = item
		}
	}
	return software, advisories
}

// confirmedExposureRecord checks the evidence relationship, not the semantic
// truth of the worker's package or vendor interpretation. A separate supported
// challenge remains mandatory for the finding status.
func confirmedExposureRecord(state State, finding Finding) bool {
	if finding.AdvisoryAssessmentID == "" || len(finding.AffectedSoftware) == 0 {
		return false
	}
	software, advisories := recordedResearch(state)
	advisory, ok := advisories[finding.AdvisoryAssessmentID]
	if !ok || advisory.Applicability != "affected" || !strings.HasPrefix(advisory.ID, finding.ValidationTask+":") {
		return false
	}
	matchingID := len(finding.CVEIDs) == 0
	for _, id := range finding.CVEIDs {
		matchingID = matchingID || strings.EqualFold(id, advisory.AdvisoryID)
	}
	observation, ok := software[advisory.SoftwareID]
	if !matchingID || !ok || observation.Version == "" || !researchrecord.StrongIdentity(observation.EvidenceKind) {
		return false
	}
	for _, ref := range observation.Evidence {
		if !slices.Contains(finding.Evidence, ref) {
			return false
		}
	}
	for _, ref := range advisory.Evidence {
		if !slices.Contains(finding.References, ref) {
			return false
		}
	}
	return true
}

// SupportedExposureForFinding lets report and analysis surfaces apply the same
// provenance check to saved sessions without changing their historical data.
func SupportedExposureForFinding(state State, finding Finding) bool {
	return confirmedExposureRecord(state, finding)
}

// AdvisoryForFinding returns the saved advisory interpretation linked by a
// finding, including its public or vendor identifier for report display.
func AdvisoryForFinding(state State, finding Finding) (researchrecord.AdvisoryAssessment, bool) {
	_, advisories := recordedResearch(state)
	item, ok := advisories[finding.AdvisoryAssessmentID]
	return item, ok
}
