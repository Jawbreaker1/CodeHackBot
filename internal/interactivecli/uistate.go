package interactivecli

import (
	"strings"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
)

type UIState struct {
	Started bool
	Packet  ctxpacket.WorkerPacket
	Stream  []string

	Model         string
	WorkingDir    string
	ApprovalState string
}

func NewUIState() UIState {
	return UIState{
		Stream: []string{"BirdHackBot interactive session."},
	}
}

func (s *UIState) View() ViewState {
	view := BuildViewState(s.Packet, shellSessionStatus(s.Started, s.Packet))
	if strings.TrimSpace(view.Model) == "" {
		view.Model = strings.TrimSpace(s.Model)
	}
	if strings.TrimSpace(view.WorkingDir) == "" {
		view.WorkingDir = strings.TrimSpace(s.WorkingDir)
	}
	if strings.TrimSpace(view.ApprovalState) == "" {
		view.ApprovalState = strings.TrimSpace(s.ApprovalState)
	}
	return view
}

func (s *UIState) SetEnvironment(model, workingDir, approvalState string) {
	s.Model = strings.TrimSpace(model)
	s.WorkingDir = strings.TrimSpace(workingDir)
	s.ApprovalState = strings.TrimSpace(approvalState)
}

func (s *UIState) AddUserInput(line string) {
	s.appendStream("User", line)
}

func (s *UIState) AddAssistantReply(reply string) {
	s.appendStream("Assistant", reply)
}

func (s *UIState) AddShellCommand(line, output string, err error) {
	s.appendStream("Command", line)
	if strings.TrimSpace(output) != "" {
		s.appendStream("System", formatStreamBody(output, 12, 700))
	}
	if err != nil {
		s.appendStream("Error", err.Error())
	}
}

func (s *UIState) AddRunOutcome(packet ctxpacket.WorkerPacket, summary string, runErr error) {
	s.Packet = packet
	if strings.TrimSpace(summary) != "" {
		s.appendStream("Assistant", summary)
	}
	done := strings.TrimSpace(packet.TaskRuntime.State) == "done" && runErr == nil
	showTrace := runErr != nil || shouldShowRawResult(packet, runErr)
	if action := strings.TrimSpace(packet.LatestExecutionResult.Action); action != "" && (!done || showTrace) {
		s.appendStream("Command", action)
	}
	if showTrace {
		if running := strings.TrimSpace(packet.RunningSummary); running != "" {
			s.appendStream("State", formatStreamBody(running, 8, 700))
		}
		if result := strings.TrimSpace(packet.LatestExecutionResult.OutputSummary); result != "" {
			s.appendStream("Result", formatStreamBody(result, 10, 900))
		}
	}
	if runErr != nil {
		s.appendStream("Error", runErr.Error())
	}
}

func (s *UIState) AddProgressEvent(event workerloop.ProgressEvent, packet ctxpacket.WorkerPacket) {
	s.Packet = packet
	body := strings.TrimSpace(event.Message)
	switch event.Kind {
	case workerloop.EventPlanStarted:
		s.appendStream("System", firstNonEmpty(body, "Planning started."))
	case workerloop.EventPlanFinished:
		s.appendStream("System", firstNonEmpty(body, "Plan updated."))
	case workerloop.EventExecutionStarted:
		if action := strings.TrimSpace(event.Action); action != "" {
			s.appendStream("Command", action)
			return
		}
		s.appendStream("System", firstNonEmpty(body, "Execution started."))
	case workerloop.EventExecutionFinished:
		s.appendStream("System", firstNonEmpty(body, "Execution finished."))
	case workerloop.EventPostExecEvalStarted:
		s.appendStream("System", firstNonEmpty(body, "Evaluating execution result."))
	case workerloop.EventTaskFailed:
		s.appendStream("Error", firstNonEmpty(body, "Worker task failed."))
	}
}

func (s *UIState) appendStream(kind, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	s.Stream = append(s.Stream, kind+": "+body)
}

func formatStreamBody(body string, maxLines, maxChars int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if maxChars > 0 && len(body) > maxChars {
		body = strings.TrimSpace(body[:maxChars]) + "..."
	}
	if maxLines > 0 {
		lines := strings.Split(body, "\n")
		if len(lines) > maxLines {
			lines = append(lines[:maxLines], "...")
		}
		body = strings.Join(lines, "\n")
	}
	return body
}
