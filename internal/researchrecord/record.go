// Package researchrecord holds bounded, evidence-linked software and advisory
// observations shared by assessment workers and their coordinator.
package researchrecord

// SoftwareObservation records what was observed on the assessed deployment.
// EvidenceKind describes the observation method, not a claim of accuracy.
type SoftwareObservation struct {
	ID           string   `json:"id"`
	Product      string   `json:"product"`
	Component    string   `json:"component,omitempty"`
	Version      string   `json:"version,omitempty"`
	EvidenceKind string   `json:"evidence_kind"` // package_inventory, build_metadata, vendor_attestation, banner, source, inference
	Evidence     []string `json:"evidence"`
}

// AdvisoryAssessment ties an advisory to one software observation. Its
// applicability is a worker conclusion, not proof that an exploit succeeded.
type AdvisoryAssessment struct {
	ID            string   `json:"id"`
	AdvisoryID    string   `json:"advisory_id"`
	SoftwareID    string   `json:"software_id"`
	Applicability string   `json:"applicability"` // affected, not_affected, unknown
	Reason        string   `json:"reason"`
	Evidence      []string `json:"evidence"`
}

func StrongIdentity(kind string) bool {
	switch kind {
	case "package_inventory", "build_metadata", "vendor_attestation":
		return true
	default:
		return false
	}
}
