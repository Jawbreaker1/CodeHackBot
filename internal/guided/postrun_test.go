package guided

import "testing"

func TestParsePostRunReplyKeepsContinuationWithMessageField(t *testing.T) {
	reply := parsePostRunReply(`{"continue_assessment":true,"message":"I will prepare a bounded plan."}`)
	if !reply.ContinueAssessment || reply.Text != "I will prepare a bounded plan." {
		t.Fatalf("lost a valid continuation: %+v", reply)
	}
}
