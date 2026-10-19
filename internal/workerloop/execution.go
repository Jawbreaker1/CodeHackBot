package workerloop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
)

func (l Loop) execute(ctx context.Context, current *ctxpacket.WorkerPacket, response Response) (bool, error) {
	isMCP := response.Type == "mcp_list" || response.Type == "mcp_describe" || response.Type == "mcp_call"
	var recorded func(io.Writer, io.Writer) error
	if isMCP {
		if l.MCP == nil || !l.MCP.HasServer(response.MCPServer) {
			current.RunningSummary = "MCP action rejected before execution: server is not configured"
			return false, l.capture(current.Budget.Used, "post-validation", *current)
		}
		invocation := "mcp " + response.Type + " " + response.MCPServer + " [server: " + l.MCP.LaunchSpec(response.MCPServer) + "]"
		if response.MCPTool != "" {
			invocation += "/" + response.MCPTool
		}
		if response.Type == "mcp_list" {
			invocation += fmt.Sprintf(" offset=%d", response.MCPOffset)
		}
		if response.Type == "mcp_call" {
			invocation += " arguments=" + string(response.MCPArguments)
		}
		response.Command = invocation
		if response.Summary == "" {
			response.Summary = "Inspect the configured MCP server's advertised tools."
		}
		if response.Target == "" {
			response.Target = "MCP server " + response.MCPServer
		}
		if response.Impact == "" {
			response.Impact = "Starts the configured local MCP server and reads its tool metadata. Server startup may have effects outside this request."
		}
		if response.Risk == "" {
			response.Risk = "unknown"
		}
		recorded = func(stdout, stderr io.Writer) error {
			var data []byte
			var err error
			switch response.Type {
			case "mcp_list":
				data, err = l.MCP.List(ctx, response.MCPServer, response.MCPOffset)
			case "mcp_describe":
				data, err = l.MCP.Describe(ctx, response.MCPServer, response.MCPTool)
			case "mcp_call":
				data, err = l.MCP.Call(ctx, response.MCPServer, response.MCPTool, response.MCPArguments)
			}
			if len(data) > 0 {
				_, _ = stdout.Write(data)
			}
			if err != nil {
				_, _ = fmt.Fprintln(stderr, err)
			}
			return err
		}
	}
	if response.Type == "browser_run" {
		prepared, failure := prepareBrowserRun(response, current.OperatorState.WorkingDir)
		if failure != nil {
			current.RunningSummary = "Browser action rejected before execution: " + failure.OutputSummary
			return false, l.capture(current.Budget.Used, "post-validation", *current)
		}
		response = prepared
	}
	if response.Type == "metasploit_lookup" {
		prepared, failure := prepareMetasploitLookup(response, current.OperatorState.WorkingDir, l.Executor.LogDir)
		if failure != nil {
			current.RunningSummary = "Metasploit lookup rejected before execution: " + failure.OutputSummary
			return false, l.capture(current.Budget.Used, "post-validation", *current)
		}
		response = prepared
	}
	if err := l.emitWithRationale(EventActionProposed, *current, response.Command, response.Summary); err != nil {
		return false, err
	}
	if l.Approver == nil {
		return false, fmt.Errorf("execution requires an approver")
	}
	var plan execx.Plan
	var err error
	if isMCP {
		plan, err = l.Executor.PlanRecorded(response.Command, current.OperatorState.WorkingDir)
	} else {
		action, failure := prepareAction(response, current.OperatorState.WorkingDir)
		if failure != nil {
			current.RunningSummary = "Action rejected before execution: " + failure.OutputSummary
			return false, l.capture(current.Budget.Used, "post-validation", *current)
		}
		plan, err = l.Executor.Plan(action)
	}
	if err != nil {
		return false, fmt.Errorf("prepare execution: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	request := approval.Request{Command: plan.ActualExec, UseShell: plan.Action.UseShell, Cwd: plan.Action.Cwd, Impact: response.Impact, Summary: response.Summary, Target: response.Target, Risk: response.Risk}
	request = l.reviewExecutionRisk(ctx, request)
	decision, err := l.Approver.Approve(ctx, request)
	if err != nil {
		return false, fmt.Errorf("approval failed: %w", err)
	}
	auditPath := plan.LogPath + ".approval.json"
	audit, err := json.MarshalIndent(struct {
		At       time.Time
		Request  approval.Request
		Decision approval.Decision
	}{time.Now().UTC(), request, decision}, "", "  ")
	if err != nil {
		return false, err
	}
	if err = os.WriteFile(auditPath, audit, 0600); err != nil {
		return false, fmt.Errorf("record approval: %w", err)
	}
	current.OperatorState.ApprovalState = string(decision)
	if decision != approval.DecisionApproveOnce && decision != approval.DecisionApproveSession {
		current.DeniedExecution = &ctxpacket.DeniedExecution{
			Summary: request.Summary, Target: request.Target, Impact: request.Impact,
			Command: request.Command, AuditRef: auditPath,
		}
		completed := len(current.RelevantRecentResults)
		if current.LatestExecutionResult.Action != "" {
			completed++
		}
		current.TaskRuntime.State = "blocked"
		current.RunningSummary = fmt.Sprintf("The operator declined the proposed action %q; it did not execute. %d earlier tool results remain recorded for the coordinator to review before replanning or reporting. The denied action adds no coverage. Approval record: %s", request.Summary, completed, auditPath)
		if err := l.emit(EventExecutionDenied, *current, current.RunningSummary); err != nil {
			return false, err
		}
		if err := l.capture(current.Budget.Used, "post-denial", *current); err != nil {
			return false, err
		}
		return false, fmt.Errorf("execution denied by user")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	current.OperatorState.PendingAction = plan.Requested
	current.OperatorState.PendingMode = plan.ExecutionMode
	current.OperatorState.PendingExec = plan.ActualExec
	current.OperatorState.PendingLog = plan.LogPath
	current.OperatorState.PendingArtifacts = append([]string(nil), response.Artifacts...)
	if err := l.capture(current.Budget.Used, "pre-action", *current); err != nil {
		return false, err
	}
	// Recording failure is a stop condition before any external side effect.
	if err := l.emit(EventExecutionStarted, *current, "execution started"); err != nil {
		return false, err
	}
	var result execx.Result
	var execErr error
	if isMCP {
		result, execErr = l.Executor.RunRecorded(ctx, plan, recorded)
	} else {
		result, execErr = l.Executor.RunPlanned(ctx, plan)
	}
	if execErr != nil && result.LogPath == "" {
		return false, fmt.Errorf("execution evidence unavailable: %w", execErr)
	}
	declaredArtifacts, artifactErr := registerArtifacts(current.OperatorState.WorkingDir, response.Artifacts)
	current.OperatorState.PendingAction, current.OperatorState.PendingMode = "", ""
	current.OperatorState.PendingExec, current.OperatorState.PendingLog = "", ""
	current.OperatorState.PendingArtifacts = nil
	declaredArtifacts = append(declaredArtifacts, auditPath)
	evidence := combineSummaries(result.StdoutSummary, result.StderrSummary)
	if artifactErr != nil {
		evidence = combineSummaries(evidence, "Declared artifact was not registered: "+artifactErr.Error())
	}
	if current.LatestExecutionResult.Action != "" {
		current.RelevantRecentResults = append([]ctxpacket.ExecutionResult{current.LatestExecutionResult}, current.RelevantRecentResults...)
	}
	current.LatestExecutionResult = ctxpacket.ExecutionResult{
		Action: result.Action, ActualExec: result.ActualExec, ExecutionMode: result.ExecutionMode,
		Cwd: result.Cwd, StartedAt: result.StartedAt, FinishedAt: result.FinishedAt,
		ExitStatus: fmt.Sprint(result.ExitStatus), OutputSummary: compactOutputSummary(evidence),
		OutputEvidence: evidence, LogRefs: []string{result.LogPath}, ArtifactRefs: append(append([]string(nil), result.ArtifactRefs...), declaredArtifacts...),
		Assessment: result.Assessment, Signals: result.Signals, FailureClass: result.FailureClass,
	}
	current.RunningSummary = buildRunningSummary(current.SessionFoundation.Goal, current.LatestExecutionResult, nil)
	if err := l.emit(EventExecutionFinished, *current, "execution finished"); err != nil {
		return false, err
	}
	if err := l.capture(current.Budget.Used, "post-action", *current); err != nil {
		return false, err
	}
	// A failed command is an observation for the model, not a worker failure.
	return true, ctx.Err()
}
