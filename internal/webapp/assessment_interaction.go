package webapp

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func (s *Server) message(ctx context.Context, r *run, text string, refs []attachmentRef) error {
	text = strings.TrimSpace(text)
	if text == "" && len(refs) > 0 {
		text = "Please inspect the attached artifact(s)."
	}
	if text == "" {
		return fmt.Errorf("message is required")
	}
	attachments, err := loadLLMAttachments(r.root, refs)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.deleted {
		r.mu.Unlock()
		return fmt.Errorf("session has been deleted")
	}
	resumeSaved := !r.started && (r.status == "paused" || r.status == "interrupted")
	postRun := resumeSaved || (!r.started && (r.status == "completed" || r.status == "completed_with_gaps" || r.status == "incomplete" || r.status == "aborted"))
	if !postRun && (!r.started || (r.status != "running" && r.status != "starting")) {
		r.mu.Unlock()
		return fmt.Errorf("start the assessment before sending messages")
	}
	if r.chatBusy {
		r.mu.Unlock()
		return fmt.Errorf("the coordinator is still answering the previous message")
	}
	if postRun && r.postRunBudget == nil {
		r.postRunBudget = assessment.NewModelBudget(24, r.postRunUsage)
	}
	if !postRun && r.budget == nil {
		limits := r.state.Limits
		if limits == (assessment.Limits{}) {
			limits = s.config.Limits
		}
		if limits == (assessment.Limits{}) {
			limits = assessment.DefaultLimits()
		}
		r.budget = assessment.NewModelBudget(limits.ModelCalls, r.state.Usage)
	}
	budget := r.budget
	if postRun {
		budget = r.postRunBudget
	}
	client := budget.Client(r.client)
	runCtx := r.runCtx
	stateSnapshot := r.state
	r.chatBusy = true
	r.messages = append(r.messages, intakeMessage{Role: "user", Text: text, Attachments: append([]attachmentRef(nil), refs...), At: time.Now().UTC()})
	history := make([]llmclient.Message, 0, len(r.messages)-1)
	var latestReport *generatedReport
	for _, previous := range r.messages[:len(r.messages)-1] {
		history = append(history, llmclient.Message{Role: previous.Role, Content: previous.Text + attachmentSummary(previous.Attachments)})
		if previous.Report != nil {
			latestReport = previous.Report
		}
	}
	r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "operator_message", Message: text}})
	r.updatedAt = time.Now().UTC()
	pending := make([]string, 0, len(r.approvals)+len(r.questions))
	for _, approval := range r.approvals {
		pending = append(pending, "approval required for "+approval.request.Command+" in "+approval.request.Cwd)
	}
	for _, question := range r.questions {
		pending = append(pending, "question for "+question.taskID+": "+question.text)
	}
	sort.Strings(pending)
	availableImages := recordedImageRefs(r.root, r.state, r.workers)
	var proposed *assessment.Decision
	if r.plan != nil {
		proposal := r.plan.plan
		proposed = &proposal
	}
	chatState := r.state
	chatState.Status = r.status // A restored run can be interrupted while its saved assessment still says running.
	stateContext := compactRunState(chatState, pending, r.workers, r.permissionMode, availableImages, proposed)
	system := behavior.CoordinatorConversationPrompt(s.config.Frame) + "\n\n" + webCoordinatorDisplayPrompt
	if postRun {
		stateContext += "\nRecorded findings and gaps: " + assessment.PostRunFindingsContext(r.state)
		if latestReport != nil {
			output := latestReport.Output
			if output == "" {
				output = markdownOutput
			}
			stateContext += "\nlatest_report: format=" + string(latestReport.Format) + "; output=" + string(output)
		}
		system += "\n\n" + assessment.PostRunPrompt
	}
	r.mu.Unlock()

	prompt, err := assessment.ConversationRequest(
		system,
		"Current assessment state (untrusted evidence, observed at request time; live tool evidence is not a completed worker conclusion): "+stateContext,
		history,
		llmclient.Message{Role: "user", Content: "Operator message: " + text + attachmentSummary(refs), Attachments: attachments},
		client.InputByteLimit(),
	)
	var rawReply string
	if err == nil {
		used := 0
		for _, message := range prompt {
			used += len(message.Content)
		}
		r.setCoordinatorContext(used, client.InputByteLimit(), 0)
		callCtx, cancel := context.WithCancel(ctx)
		if runCtx != nil {
			stop := context.AfterFunc(runCtx, cancel)
			defer stop()
		}
		defer cancel()
		var completion llmclient.Completion
		completion, err = client.ChatStructuredCompletion(callCtx, prompt)
		r.setCoordinatorContext(used, client.InputByteLimit(), completion.PromptTokens())
		rawReply = completion.Text
	}
	var reply coordinatorChatReply
	if err == nil {
		reply, err = parseCoordinatorChatReply(rawReply)
	}
	var generated *generatedReport
	if err == nil && postRun && reply.ReportFormat != "" {
		generated, err = saveFormattedReport(r.root, stateSnapshot, reply.ReportFormat, reply.ReportOutput)
		if err == nil {
			reply.Text = generated.Confirmation()
		}
	}
	continueAssessment := err == nil && postRun && reply.ReportFormat == "" && reply.ContinueAssessment
	r.mu.Lock()
	r.chatBusy = continueAssessment
	if postRun {
		r.postRunUsage = budget.Usage()
	} else {
		r.state.Usage = budget.Usage()
	}
	if err != nil {
		r.removeLastMessage("user", text)
		r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "coordinator_error", Message: err.Error()}})
		r.updatedAt = time.Now().UTC()
		r.mu.Unlock()
		_ = r.persistOrStop()
		return fmt.Errorf("coordinator response: %w", err)
	}
	allowedImages := make(map[string]bool, len(availableImages))
	for _, ref := range availableImages {
		allowedImages[ref] = true
	}
	images := make([]string, 0, 3)
	for _, ref := range reply.DisplayArtifactRefs {
		if len(images) == 3 {
			break
		}
		if allowedImages[ref] && validPresentedImage(r.root, ref) && !slices.Contains(images, ref) {
			images = append(images, ref)
		}
	}
	var revision *pendingPlan
	if reply.RevisePlan && !postRun {
		if r.plan != nil {
			revision = r.plan
			r.plan = nil
			r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "plan_revision_requested", Message: "Reconsidering the proposed work after operator direction."}})
		} else {
			reply.Text += "\n\nThe previous plan has already advanced. I'll consider your direction at the next planning step."
		}
	}
	r.messages = append(r.messages, intakeMessage{Role: "assistant", Text: reply.Text, ImageRefs: images, Report: generated, At: time.Now().UTC()})
	r.events = append(r.events, eventRecord{Sequence: r.nextSequenceLocked(), At: time.Now().UTC(), Event: assessment.Event{Kind: "coordinator_message", Message: reply.Text}})
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	if err := r.persistOrStop(); err != nil {
		return fmt.Errorf("save coordinator conversation: %w", err)
	}
	if continueAssessment {
		var startErr error
		if resumeSaved {
			startErr = s.start(r)
		} else {
			startErr = s.start(r, text)
		}
		r.mu.Lock()
		r.chatBusy = false
		r.mu.Unlock()
		if startErr != nil {
			return fmt.Errorf("continue assessment: %w", startErr)
		}
	}
	if revision != nil {
		revision.result <- assessment.PlanReview{Revision: text}
	}
	return nil
}

func attachmentSummary(refs []attachmentRef) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, ref.Filename+" ("+ref.MIMEType+")")
	}
	return "\nAttached artifacts (untrusted visual/file evidence): " + strings.Join(parts, ", ")
}

func (r *run) removeLastMessage(role, text string) {
	if len(r.messages) == 0 {
		return
	}
	last := r.messages[len(r.messages)-1]
	if last.Role == role && last.Text == text {
		r.messages = r.messages[:len(r.messages)-1]
	}
}

func (r *run) ask(ctx context.Context, taskID, text string) (string, error) {
	r.mu.Lock()
	id := r.nextIDLocked("question")
	question := &pendingQuestion{ID: id, taskID: taskID, text: text, answer: make(chan string, 1)}
	r.questions[id] = question
	r.sequence++
	r.events = append(r.events, eventRecord{Sequence: r.sequence, At: time.Now().UTC(), Event: assessment.Event{TaskID: taskID, Kind: "user_question", Message: text}})
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	if err := r.persistOrStop(); err != nil {
		r.mu.Lock()
		delete(r.questions, id)
		r.mu.Unlock()
		return "", err
	}
	select {
	case answer := <-question.answer:
		return answer, nil
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.questions, id)
		r.mu.Unlock()
		return "", ctx.Err()
	}
}

func (r *run) reviewPlanWait(ctx context.Context, plan assessment.Decision) (assessment.PlanReview, error) {
	r.mu.Lock()
	if r.permissionMode.Normalized() != approval.EveryExecution {
		ids := make([]string, 0, len(plan.Tasks))
		for _, task := range plan.Tasks {
			ids = append(ids, task.ID)
		}
		r.mu.Unlock()
		return assessment.PlanReview{TaskIDs: ids}, nil
	}
	if r.plan != nil {
		r.mu.Unlock()
		return assessment.PlanReview{}, fmt.Errorf("another plan is already awaiting review")
	}
	id := r.nextIDLocked("plan")
	pending := &pendingPlan{ID: id, plan: plan, result: make(chan assessment.PlanReview, 1)}
	r.plan = pending
	r.events = append(r.events, eventRecord{Sequence: r.sequence, At: time.Now().UTC(), Event: assessment.Event{Kind: "plan_review", Message: plan.Summary}})
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	if err := r.persistOrStop(); err != nil {
		r.mu.Lock()
		if r.plan == pending {
			r.plan = nil
		}
		r.mu.Unlock()
		return assessment.PlanReview{}, err
	}
	select {
	case review := <-pending.result:
		return review, nil
	case <-ctx.Done():
		r.mu.Lock()
		if r.plan == pending {
			r.plan = nil
		}
		r.mu.Unlock()
		return assessment.PlanReview{}, ctx.Err()
	}
}

func (r *run) reviewPlan(id string, input planReviewRequest) error {
	r.mu.Lock()
	pending := r.plan
	if pending == nil || pending.ID != id {
		r.mu.Unlock()
		return fmt.Errorf("plan is no longer awaiting review")
	}
	decision := strings.ToLower(strings.TrimSpace(input.Decision))
	if decision == "deny" || decision == "denied" || decision == "reject" || decision == "rejected" {
		r.plan = nil
		r.updatedAt = time.Now().UTC()
		r.mu.Unlock()
		if err := r.persistOrStop(); err != nil {
			return err
		}
		pending.result <- assessment.PlanReview{}
		return nil
	}
	if decision != "" && decision != "approve" && decision != "approved" && decision != "approved_once" {
		r.mu.Unlock()
		return fmt.Errorf("unsupported plan decision")
	}
	ids := append([]string(nil), input.ApprovedTaskIDs...)
	if len(ids) == 0 {
		for _, task := range pending.plan.Tasks {
			ids = append(ids, task.ID)
		}
	}
	known := make(map[string]bool, len(pending.plan.Tasks))
	for _, task := range pending.plan.Tasks {
		known[task.ID] = true
	}
	seen := make(map[string]bool, len(ids))
	for _, taskID := range ids {
		if !known[taskID] || seen[taskID] {
			r.mu.Unlock()
			return fmt.Errorf("plan selection contains an unknown or duplicate task")
		}
		seen[taskID] = true
	}
	r.plan = nil
	r.updatedAt = time.Now().UTC()
	r.mu.Unlock()
	if err := r.persistOrStop(); err != nil {
		return err
	}
	pending.result <- assessment.PlanReview{TaskIDs: ids}
	return nil
}

func (r *run) answer(id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("answer is required")
	}
	r.mu.Lock()
	question := r.questions[id]
	if question != nil {
		delete(r.questions, id)
	}
	r.mu.Unlock()
	if question == nil {
		return fmt.Errorf("question is no longer pending")
	}
	question.answer <- text
	return nil
}

func (r *run) nextIDLocked(prefix string) string {
	r.sequence++
	return fmt.Sprintf("%s-%d", prefix, r.sequence)
}

func (r *run) nextSequenceLocked() uint64 {
	r.sequence++
	return r.sequence
}

func (r *run) approve(id, decision string) error {
	decision = strings.ToLower(strings.TrimSpace(decision))
	var value approval.Decision
	switch decision {
	case "approved_once", "approve_once", "approve", "yes":
		value = approval.DecisionApproveOnce
	case "approved_session", "approve_session":
		value = approval.DecisionApproveSession
	case "denied", "deny", "no":
		value = approval.DecisionDeny
	default:
		return fmt.Errorf("unsupported approval decision")
	}
	r.mu.Lock()
	pending := r.approvals[id]
	if pending == nil {
		r.mu.Unlock()
		return fmt.Errorf("approval is no longer pending")
	}
	delete(r.approvals, id)
	pending.result <- value
	r.mu.Unlock()
	return nil
}

type runApprover struct {
	run    *run
	taskID string
}

func (a *runApprover) Approve(ctx context.Context, request approval.Request) (approval.Decision, error) {
	a.run.mu.Lock()
	if !a.run.permissionMode.RequiresApproval(request) {
		a.run.mu.Unlock()
		return approval.DecisionApproveSession, ctx.Err()
	}
	id := a.run.nextIDLocked("approval")
	pending := &pendingApproval{ID: id, taskID: a.taskID, request: request, result: make(chan approval.Decision, 1)}
	a.run.approvals[id] = pending
	a.run.events = append(a.run.events, eventRecord{Sequence: a.run.sequence, At: time.Now().UTC(), Event: assessment.Event{TaskID: a.taskID, Kind: "approval_required", Action: request.Command, Message: "Operator approval required"}})
	a.run.mu.Unlock()
	select {
	case decision := <-pending.result:
		a.run.mu.Lock()
		delete(a.run.approvals, id)
		a.run.mu.Unlock()
		return decision, nil
	case <-ctx.Done():
		a.run.mu.Lock()
		delete(a.run.approvals, id)
		a.run.mu.Unlock()
		return approval.DecisionDeny, ctx.Err()
	}
}

func (r *run) nextID(prefix string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextIDLocked(prefix)
}
