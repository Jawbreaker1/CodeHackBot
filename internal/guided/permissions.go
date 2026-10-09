package guided

import (
	"context"
	"errors"
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
	c.BeginModal("Approval settings")
	defer c.EndModal()
	current := c.approvalMode()
	selected := 0
	switch current {
	case approval.DangerousOnly:
		selected = 1
	case approval.FullAccess:
		selected = 2
	}
	choice, err := c.Choose(ctx, "Approval level", "Choose an approval level, or Esc to close", []consoleChoice{
		{Label: "Approve every execution", Detail: "Ask before each tool action", Value: "1"},
		{Label: "Review risky actions", Detail: "Low-risk actions run automatically", Value: "2"},
		{Label: "Approve everything", Detail: "Run without execution prompts", Value: "3"},
	}, selected)
	if err != nil {
		if errors.Is(err, errModalCanceled) {
			return nil
		}
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
	if mode == current {
		return nil
	}
	if mode != approval.EveryExecution {
		answer, err := c.Ask(ctx, "Enable "+mode.Label()+" for this session? Any pending execution covered by this mode will proceed. Type confirm to apply.")
		if err != nil {
			if errors.Is(err, errModalCanceled) {
				return nil
			}
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
	c.Notice("Approval setting: %s. Covered pending executions will proceed.", mode.Label())
	return nil
}
