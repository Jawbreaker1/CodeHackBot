package cli

import (
	"errors"
	"fmt"
	"strings"
)

var errOperatorInterrupted = errors.New("operator interrupted execution")
var errExecutionApprovalRequired = errors.New("execution approval required")

func operatorInterruptedError() error {
	return fmt.Errorf("%w", errOperatorInterrupted)
}

func isOperatorInterrupted(err error) bool {
	return errors.Is(err, errOperatorInterrupted)
}

func executionApprovalRequiredError() error {
	return fmt.Errorf("%w", errExecutionApprovalRequired)
}

func isExecutionApprovalRequired(err error) bool {
	return errors.Is(err, errExecutionApprovalRequired)
}

func (r *Runner) handleOperatorInterrupt(goal string) {
	msg := "Execution aborted by operator (Ctrl-C). Assist loop stopped. Reply `continue` to resume, or provide a new instruction."
	safePrintln(msg)
	r.appendConversation("Assistant", msg)
	r.pendingAssistGoal = strings.TrimSpace(goal)
	r.pendingAssistQ = msg
	r.assistExecApproved = false
	if goal != "" {
		r.assistExecGoal = goal
	}
}
