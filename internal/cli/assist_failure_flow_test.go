package cli

import "testing"

func TestShouldPauseAfterHandledFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		dryRun          bool
		pendingGoal     string
		pendingQuestion string
		want            bool
	}{
		{
			name:   "dry run always pauses",
			dryRun: true,
			want:   true,
		},
		{
			name:            "pending question pauses",
			pendingQuestion: "need target",
			want:            true,
		},
		{
			name:        "pending goal pauses",
			pendingGoal: "scan host",
			want:        true,
		},
		{
			name: "no pending state continues loop",
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldPauseAfterHandledFailure(tc.dryRun, tc.pendingGoal, tc.pendingQuestion)
			if got != tc.want {
				t.Fatalf("shouldPauseAfterHandledFailure(dryRun=%v, pendingGoal=%q, pendingQuestion=%q)=%v want %v", tc.dryRun, tc.pendingGoal, tc.pendingQuestion, got, tc.want)
			}
		})
	}
}
