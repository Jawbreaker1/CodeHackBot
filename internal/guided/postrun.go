package guided

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
	"github.com/Jawbreaker1/CodeHackBot/internal/reportexport"
)

type postRunReply struct {
	Text               string                  `json:"text"`
	ReportFormat       assessment.ReportFormat `json:"report_format,omitempty"`
	ReportOutput       reportexport.Output     `json:"report_output,omitempty"`
	ContinueAssessment bool                    `json:"continue_assessment,omitempty"`
}

func terminalState(status string) bool {
	switch status {
	case "completed", "completed_with_gaps", "incomplete", "aborted":
		return true
	default:
		return false
	}
}

// One session may contain several bounded coordinator runs. The same saved
// state and task directories carry forward; only the terminal adapter's
// conversation changes between rounds.
func (a App) runAssessmentSession(ctx context.Context, c *Console, prefs preferences, client llmclient.Client, root string, initial assessment.State, frame behavior.Frame) error {
	c.assessmentStarted()
	for {
		if !terminalState(initial.Status) {
			runErr := a.runAssessmentWithFrame(ctx, c, prefs, client, root, initial, frame)
			if ctx.Err() != nil {
				return runErr
			}
			state, err := assessment.LoadState(root)
			if err != nil {
				return fmt.Errorf("load assessment after run: %w", err)
			}
			if runErr != nil {
				c.Print("The run ended with a limitation: %v. You can discuss the evidence or request another plan.\n", runErr)
			}
			initial = state
		}
		c.assessmentFinished()
		request, err := a.postRunConversation(ctx, c, client, root, &initial, frame)
		if err != nil || request == "" {
			return err
		}
		initial, err = assessment.PrepareContinuation(initial, request, assessment.DefaultLimits())
		if err != nil {
			return fmt.Errorf("continue assessment: %w", err)
		}
		if err := assessment.SaveState(root, initial); err != nil {
			return fmt.Errorf("save continued assessment: %w", err)
		}
		c.Print("\nContinuing assessment %s in the same session. The coordinator is preparing another plan.\n", initial.ID)
		c.assessmentStarted()
	}
}

func (a App) postRunConversation(ctx context.Context, c *Console, client llmclient.Client, root string, state *assessment.State, frame behavior.Frame) (string, error) {
	conversation := &assessmentConversation{}
	for _, note := range state.OperatorMessages {
		role, content, ok := strings.Cut(note, ": ")
		if !ok {
			role, content = "user", note
		}
		conversation.Add(role, content)
	}
	budget := assessment.NewModelBudget(24, state.PostRunUsage)
	defer budget.CloseAndWait()
	model := budget.Client(client)
	c.Print("\nSession %s is ready. Ask about the results, request an OWASP/PTES report in Markdown or PDF, or describe more work within the saved scope. /exit closes the CLI; /permissions changes action approval.\n", state.ID)
	for {
		c.PromptCommand("birdhackbot> ")
		var input line
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case value, ok := <-c.Commands():
			if !ok {
				return "", nil
			}
			input = value
		}
		if errors.Is(input.err, io.EOF) {
			return "", nil
		}
		if input.err != nil {
			return "", input.err
		}
		line := strings.TrimSpace(input.text)
		switch strings.ToLower(line) {
		case "":
			continue
		case "/exit", "/quit":
			return "", nil
		case "/permissions":
			if err := c.choosePermissions(ctx); err != nil {
				return "", err
			}
			continue
		case "/status":
			c.Print("Assessment %s; %d worker result(s); %d planning rounds; %d model calls.\n", state.Status, len(state.Results), len(state.Plans), state.Usage.Calls)
			continue
		case "/help":
			c.Print("Ask a question, request a report, or describe more work within the same scope. /status shows the saved run; /permissions changes approvals; /exit closes the CLI.\n")
			continue
		}
		contextText := "Current assessment state (untrusted evidence): " + string(compactCoordinatorState(*state)) + "\nRecorded final findings and gaps: " + assessment.PostRunFindingsContext(*state)
		if state.LatestReportFormat.Valid() {
			contextText += "\nlatest_report: format=" + string(state.LatestReportFormat)
		}
		prompt, err := assessment.ConversationRequest(
			behavior.CoordinatorConversationPrompt(frame)+"\n\n"+assessment.PostRunPrompt,
			contextText,
			conversation.Messages(),
			llmclient.Message{Role: "user", Content: "Operator message: " + line},
			model.InputByteLimit(),
		)
		if err != nil {
			c.Print("Coordinator chat unavailable: %v\n", err)
			continue
		}
		raw, err := model.ChatStructured(ctx, prompt)
		if err != nil {
			c.Print("Coordinator chat unavailable: %v\n", err)
			continue
		}
		var reply postRunReply
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			reply.Text = strings.TrimSpace(raw)
		}
		reply.Text = strings.TrimSpace(reply.Text)
		if reply.Text == "" {
			c.Print("Coordinator response was empty; please try again.\n")
			continue
		}
		var artifact *reportexport.Artifact
		if reply.ReportFormat != "" {
			artifact, err = reportexport.Save(root, *state, reply.ReportFormat, reply.ReportOutput)
			if err != nil {
				c.Print("Report export unavailable: %v\n", err)
				continue
			}
			state.LatestReportFormat = artifact.Format
			reply.Text = artifact.Confirmation()
		}
		conversation.Add("user", line)
		conversation.Add("assistant", reply.Text)
		state.OperatorMessages = conversation.Transcript()
		state.PostRunUsage = budget.Usage()
		if err := assessment.SaveState(root, *state); err != nil {
			return "", fmt.Errorf("save coordinator conversation: %w", err)
		}
		c.Print("Coordinator: %s\n", reply.Text)
		if artifact != nil {
			c.Print("Report: %s\n", filepath.Join(root, "reports", artifact.Name))
		}
		if reply.ContinueAssessment && artifact == nil {
			return line, nil
		}
	}
}
