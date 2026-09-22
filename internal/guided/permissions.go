package guided

import (
	"context"
	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"strings"
)

func (c *Console) approvalMode() approval.Mode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.permissionMode.Normalized()
}

func (a taskApprover) ApprovalMode() approval.Mode { return a.console.approvalMode() }

func (c *Console) choosePermissions(ctx context.Context) error {
	choice, err := c.Ask(ctx, "Approval settings for this session\n1. Approve every execution\n2. Approve dangerous executions (model-assessed; uncertain actions still ask)\n3. Approve everything (no execution prompts)\nChoose 1–3, or Enter to keep "+c.approvalMode().Label())
	if err != nil {
		return err
	}
	var mode approval.Mode
	switch strings.TrimSpace(choice) {
	case "1":
		mode = approval.EveryExecution
	case "2":
		mode = approval.DangerousOnly
	case "3":
		mode = approval.FullAccess
	default:
		return nil
	}
	if mode != approval.EveryExecution {
		answer, err := c.Ask(ctx, "Enable "+mode.Label()+" for this authorized VM session? Scope and prohibitions still apply. Pending actions still need a decision. Type confirm to apply.")
		if err != nil {
			return err
		}
		if strings.TrimSpace(answer) != "confirm" {
			return nil
		}
	}
	c.mu.Lock()
	c.permissionMode = mode
	c.mu.Unlock()
	c.emit(consolePermissions, mode.Label())
	c.Print("Approval setting: %s. Pending requests still need a decision.\n", mode.Label())
	return nil
}
