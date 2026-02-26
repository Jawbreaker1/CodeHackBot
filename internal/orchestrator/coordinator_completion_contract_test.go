package orchestrator

import "testing"

func TestVerifyRequiredArtifactsConcreteFilenameMatch(t *testing.T) {
	t.Parallel()

	required := []string{"zip.hash", "password_found.txt"}
	verified := []string{
		"/tmp/run/artifact/T-003/zip.hash",
		"/tmp/run/artifact/T-003/worker.log",
	}
	missing := verifyRequiredArtifacts(required, verified)
	if len(missing) != 1 || missing[0] != "password_found.txt" {
		t.Fatalf("expected only password_found.txt missing, got %v", missing)
	}
}

func TestVerifyRequiredArtifactsGenericFallback(t *testing.T) {
	t.Parallel()

	required := []string{"command log"}
	verified := []string{"/tmp/run/artifact/T-001/worker.log"}
	missing := verifyRequiredArtifacts(required, verified)
	if len(missing) != 0 {
		t.Fatalf("expected no missing generic artifacts, got %v", missing)
	}
}

func TestVerifyRequiredArtifactsRelativePathMatch(t *testing.T) {
	t.Parallel()

	required := []string{"artifact/T-003/zip.hash"}
	verified := []string{"/home/user/sessions/run-1/orchestrator/artifact/T-003/zip.hash"}
	missing := verifyRequiredArtifacts(required, verified)
	if len(missing) != 0 {
		t.Fatalf("expected relative path requirement to match absolute artifact path, got %v", missing)
	}
}
