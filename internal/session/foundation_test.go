package session

import "testing"

func TestNewFoundation(t *testing.T) {
	f, err := NewFoundation(Input{
		Goal:                 "Recover the zip password",
		ReportingRequirement: "owasp",
	})
	if err != nil {
		t.Fatalf("NewFoundation() error = %v", err)
	}

	if f.Goal != "Recover the zip password" {
		t.Fatalf("Goal = %q", f.Goal)
	}
	if f.ReportingRequirement != "owasp" {
		t.Fatalf("ReportingRequirement = %q", f.ReportingRequirement)
	}
}

func TestNewFoundationDefaultsReportingRequirement(t *testing.T) {
	f, err := NewFoundation(Input{Goal: "Test"})
	if err != nil {
		t.Fatalf("NewFoundation() error = %v", err)
	}
	if f.ReportingRequirement != "owasp" {
		t.Fatalf("ReportingRequirement = %q, want owasp", f.ReportingRequirement)
	}
}

func TestNewFoundationRequiresGoal(t *testing.T) {
	if _, err := NewFoundation(Input{}); err == nil {
		t.Fatal("expected error for empty goal")
	}
}
