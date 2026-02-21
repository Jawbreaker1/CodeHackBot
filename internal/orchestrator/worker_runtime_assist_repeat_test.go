package orchestrator

import "testing"

func TestTrackAssistActionStreakResetsOnNewAction(t *testing.T) {
	t.Parallel()

	key, streak := trackAssistActionStreak("", 0, "nmap\x1f-sV\x1f192.168.50.1")
	if key == "" || streak != 1 {
		t.Fatalf("expected initial streak 1, got key=%q streak=%d", key, streak)
	}

	key, streak = trackAssistActionStreak(key, streak, "nmap\x1f-sV\x1f192.168.50.1")
	if streak != 2 {
		t.Fatalf("expected repeated streak 2, got %d", streak)
	}

	key, streak = trackAssistActionStreak(key, streak, "grep\x1frouter")
	if key != "grep\x1frouter" || streak != 1 {
		t.Fatalf("expected streak reset for new action, got key=%q streak=%d", key, streak)
	}
}

func TestTrackAssistActionStreakEmptyActionClearsState(t *testing.T) {
	t.Parallel()

	key, streak := trackAssistActionStreak("nmap\x1f-sV", 2, "")
	if key != "" || streak != 0 {
		t.Fatalf("expected empty action to clear streak, got key=%q streak=%d", key, streak)
	}
}

func TestBuildAssistActionKeyCanonicalizesAliasAndArgOrder(t *testing.T) {
	t.Parallel()

	keyA := buildAssistActionKey("ls", []string{"-la", "."})
	keyB := buildAssistActionKey("list_dir", []string{".", "-la"})
	if keyA != keyB {
		t.Fatalf("expected semantic key match for alias/reordered args, got %q vs %q", keyA, keyB)
	}
}
