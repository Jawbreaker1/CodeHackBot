package webapp

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
)

func (r *run) view(after string) assessmentView {
	r.mu.RLock()
	defer r.mu.RUnlock()
	model := r.client.Model
	if strings.TrimSpace(model) == "" {
		model = r.state.Model
	}
	view := assessmentView{Customer: r.customer, ID: r.id, Goal: r.goal, Scope: r.scope, Approach: r.state.Approach, Status: r.status, Model: model, ModelProfile: r.profileID, ModelBusy: r.chatBusy || r.started, CanChangeModel: !r.chatBusy && !r.started && r.status == "draft", Resumable: !r.started && r.status != "draft" && r.status != "completed" && r.status != "completed_with_gaps" && r.status != "aborted", UpdatedAt: r.updatedAt, Error: r.state.Error, StartedAt: r.state.StartedAt, FinishedAt: r.state.FinishedAt, Usage: r.state.Usage, PostRunUsage: r.postRunUsage, Plans: len(r.state.Plans), Results: append([]assessment.Result(nil), r.state.Results...), Messages: messageViews(r.messages, "assessments", r.id), ReportURL: "/api/v1/assessments/" + r.id + "/report"}
	if r.persistErr != nil {
		view.Error = r.persistErr.Error()
	}
	if r.budget != nil {
		view.Usage = r.budget.Usage()
	}
	for i, message := range r.messages {
		for _, ref := range message.ImageRefs {
			if validPresentedImage(r.root, ref) {
				view.Messages[i].Images = append(view.Messages[i].Images, presentedImageView(r.id, ref))
			}
		}
	}
	if r.status != "draft" && r.status != "running" && r.status != "starting" {
		if info, err := os.Stat(filepath.Join(r.root, "report.md")); err == nil && info.Mode().IsRegular() {
			view.ReportReady = true
			if evidence, err := os.Stat(filepath.Join(r.root, "evidence-index.md")); err == nil && evidence.Mode().IsRegular() {
				view.EvidenceIndexURL = "/api/v1/assessments/" + r.id + "/evidence-index"
			}
		}
	}
	view.Limits = r.state.Limits
	view.PermissionMode = r.permissionMode.Normalized()
	view.Conclusion = assessmentConclusion(r.state)
	view.ConclusionDetail = assessmentConclusionDetail(r.state)
	for _, worker := range r.workers {
		worker.Evidence = append([]assessment.EvidenceView(nil), worker.Evidence...)
		view.Workers = append(view.Workers, worker)
	}
	sort.Slice(view.Workers, func(i, j int) bool { return view.Workers[i].ID < view.Workers[j].ID })
	view.ContextWindow, view.ContextWindows = agentContextWindows(r.state, r.coordinatorContext, view.Workers, r.status, r.started, r.chatBusy, r.events)
	view.ReportReadiness = assessment.AssessReportReadiness(r.state)
	for i := range view.Workers {
		for j := range view.Workers[i].Evidence {
			view.Workers[i].Evidence[j] = decorateEvidence(view.Workers[i].Evidence[j], r.id)
		}
	}
	view.Findings = assessment.DisplayFindings(r.state)
	view.Artifacts = assessmentArtifacts(r.root, r.id, r.state, r.workers)
	planState := r.state
	planState.Status = r.status
	view.PlanTimeline = coordinatorPlans(planState, r.workers)
	if n, err := strconv.ParseUint(strings.TrimSpace(after), 10, 64); err == nil {
		for _, event := range r.events {
			if event.Sequence > n {
				view.Events = append(view.Events, event)
			}
		}
	} else {
		view.Events = append([]eventRecord(nil), r.events...)
	}
	for i := range view.Events {
		if view.Events[i].Event.Evidence != nil {
			decorated := decorateEvidence(*view.Events[i].Event.Evidence, r.id)
			view.Events[i].Event.Evidence = &decorated
		}
	}
	for _, pending := range r.approvals {
		view.PendingApprovals = append(view.PendingApprovals, approvalView{ID: pending.ID, TaskID: pending.taskID, Command: pending.request.Command, UseShell: pending.request.UseShell, Cwd: pending.request.Cwd, Impact: pending.request.Impact, Summary: pending.request.Summary, Target: pending.request.Target, Risk: pending.request.Risk, ReviewReason: pending.request.ReviewReason})
	}
	for _, question := range r.questions {
		view.PendingQuestions = append(view.PendingQuestions, questionView{ID: question.ID, TaskID: question.taskID, Text: question.text})
	}
	if r.plan != nil {
		phase := r.plan.plan.Phase
		if phase == "" {
			phase = "assessment"
		}
		view.PendingPlan = &planApprovalView{ID: r.plan.ID, Phase: phase, Summary: r.plan.plan.Summary, PlainSummary: r.plan.plan.PlainSummary, Tasks: append([]assessment.Task(nil), r.plan.plan.Tasks...)}
		pending := coordinatorPlanView{Round: len(r.state.Plans) + 1, Phase: phase, Summary: r.plan.plan.Summary, PlainSummary: r.plan.plan.PlainSummary, Signal: "review", Status: "review", OpenLeads: append([]assessment.Lead(nil), r.plan.plan.OpenLeads...), ClosedLeads: append([]assessment.LeadClosure(nil), r.plan.plan.ClosedLeads...)}
		for _, task := range r.plan.plan.Tasks {
			pending.Tasks = append(pending.Tasks, coordinatorTaskView{ID: task.ID, Goal: task.Goal, DoneWhen: task.DoneWhen, StrategyHints: task.StrategyHints, Status: "review"})
		}
		view.PlanTimeline = append(view.PlanTimeline, pending)
	}
	sort.Slice(view.PendingApprovals, func(i, j int) bool { return view.PendingApprovals[i].ID < view.PendingApprovals[j].ID })
	sort.Slice(view.PendingQuestions, func(i, j int) bool { return view.PendingQuestions[i].ID < view.PendingQuestions[j].ID })
	return view
}

func decorateEvidence(evidence assessment.EvidenceView, assessmentID string) assessment.EvidenceView {
	evidence.ArtifactURLs = nil
	for _, ref := range evidence.ArtifactRefs {
		evidence.ArtifactURLs = append(evidence.ArtifactURLs, "/api/v1/assessments/"+url.PathEscape(assessmentID)+"/artifact?path="+url.QueryEscape(ref))
	}
	return evidence
}

func validPresentedImage(root, ref string) bool {
	if imageMIME(ref) == "" || !filepath.IsAbs(ref) || !resolvedWithin(root, ref) {
		return false
	}
	info, err := os.Stat(ref)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= maxAttachmentBytes
}

func presentedImageView(assessmentID, ref string) attachmentView {
	info, _ := os.Stat(ref)
	return attachmentView{
		Filename: filepath.Base(ref), MIMEType: imageMIME(ref), Bytes: info.Size(),
		URL: "/api/v1/assessments/" + url.PathEscape(assessmentID) + "/artifact?path=" + url.QueryEscape(ref),
	}
}

func (r *run) writeView(w http.ResponseWriter, after string) {
	writeJSON(w, http.StatusOK, r.view(after))
}
